package panel

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	drainBatchLines   = 500
	drainBatchBytes   = 1 << 20
	drainMessageBytes = 64 << 10
	drainPassBatches  = 20 // per app per pass, so a backlog in one app can't starve the others
	drainTick         = 5 * time.Second
	drainMaxBackoff   = 5 * time.Minute
)

// LogDrain sends a team's app logs to an HTTP endpoint. Delivery is at-least-once: lines are read from the saved log files and the cursor only moves after the endpoint accepts them, so a restart or an outage loses nothing the saved logs still hold.
type LogDrain struct {
	Team        string
	URL         string
	HeaderName  string
	HeaderValue string
	LastOK      int64
	LastError   string
}

type drainEntry struct {
	Timestamp string `json:"timestamp"`
	Message   string `json:"message"`
	App       string `json:"app"`
	Stream    string `json:"stream"`
	Container string `json:"container"`
}

type drainCursor struct {
	Day string
	Pos int64
}

func scanLogDrain(row interface{ Scan(...any) error }) (LogDrain, bool) {
	var d LogDrain
	if row.Scan(&d.Team, &d.URL, &d.HeaderName, &d.HeaderValue, &d.LastOK, &d.LastError) != nil {
		return LogDrain{}, false
	}
	return d, true
}

const logDrainCols = `team,url,header_name,header_value,last_ok,last_error`

func getLogDrain(team string) (LogDrain, bool) {
	return scanLogDrain(sqlDB.QueryRow(`SELECT `+logDrainCols+` FROM log_drains WHERE team=?`, team))
}

func allLogDrains() []LogDrain {
	rows, err := sqlDB.Query(`SELECT ` + logDrainCols + ` FROM log_drains`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []LogDrain
	for rows.Next() {
		if d, ok := scanLogDrain(rows); ok {
			out = append(out, d)
		}
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

// saveLogDrain stores the drain; a new destination starts from now rather than receiving the logs already saved.
func saveLogDrain(d LogDrain) error {
	prev, had := getLogDrain(d.Team)
	if had && prev.URL != d.URL {
		d.LastOK, d.LastError = 0, ""
	}
	_, err := sqlDB.Exec(`INSERT INTO log_drains(`+logDrainCols+`) VALUES(?,?,?,?,?,?)
		ON CONFLICT(team) DO UPDATE SET url=excluded.url, header_name=excluded.header_name,
			header_value=excluded.header_value, last_ok=excluded.last_ok, last_error=excluded.last_error`,
		d.Team, d.URL, d.HeaderName, d.HeaderValue, d.LastOK, d.LastError)
	if err != nil {
		return err
	}
	if !had || prev.URL != d.URL {
		for _, app := range teamApps(d.Team) {
			if cur, ok := logEnd(app); ok {
				setDrainCursor(d.Team, app, cur)
			}
		}
	}
	return nil
}

func deleteLogDrain(team string) {
	_, _ = sqlDB.Exec(`DELETE FROM log_drains WHERE team=?`, team)
	_, _ = sqlDB.Exec(`DELETE FROM log_drain_cursors WHERE team=?`, team)
}

func noteDrainResult(team string, sent bool, err error) {
	switch {
	case err != nil:
		_, _ = sqlDB.Exec(`UPDATE log_drains SET last_error=? WHERE team=?`, err.Error(), team)
	case sent:
		_, _ = sqlDB.Exec(`UPDATE log_drains SET last_ok=?, last_error='' WHERE team=?`, time.Now().Unix(), team)
	}
}

func teamApps(team string) []string {
	rows, err := sqlDB.Query(`SELECT app FROM sources WHERE team=? ORDER BY app`, team)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		if rows.Scan(&a) == nil {
			out = append(out, a)
		}
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

func getDrainCursor(team, app string) drainCursor {
	var c drainCursor
	_ = sqlDB.QueryRow(`SELECT day,pos FROM log_drain_cursors WHERE team=? AND app=?`, team, app).Scan(&c.Day, &c.Pos)
	return c
}

func setDrainCursor(team, app string, c drainCursor) {
	_, _ = sqlDB.Exec(`INSERT INTO log_drain_cursors(team,app,day,pos) VALUES(?,?,?,?)
		ON CONFLICT(team,app) DO UPDATE SET day=excluded.day, pos=excluded.pos`, team, app, c.Day, c.Pos)
}

func logDays(app string) []string {
	files, _ := filepath.Glob(filepath.Join(appLogDir(app), "*.log"))
	days := make([]string, 0, len(files))
	for _, f := range files {
		days = append(days, strings.TrimSuffix(filepath.Base(f), ".log"))
	}
	sort.Strings(days)
	return days
}

// logEnd is the position just past everything saved for an app so far.
func logEnd(app string) (drainCursor, bool) {
	days := logDays(app)
	if len(days) == 0 {
		return drainCursor{}, false
	}
	day := days[len(days)-1]
	info, err := os.Stat(filepath.Join(appLogDir(app), day+".log"))
	if err != nil {
		return drainCursor{}, false
	}
	return drainCursor{Day: day, Pos: info.Size()}, true
}

// readDrainBatch reads complete lines after the cursor, across day files, and returns where the next read starts. Days pruned while a drain was down are skipped.
func readDrainBatch(app string, cur drainCursor) ([]drainEntry, drainCursor, error) {
	var out []drainEntry
	size := 0
	days := logDays(app)
	for i, day := range days {
		if day < cur.Day {
			continue
		}
		pos := int64(0)
		if day == cur.Day {
			pos = cur.Pos
		}
		f, err := os.Open(filepath.Join(appLogDir(app), day+".log"))
		if err != nil {
			return out, cur, err
		}
		if _, err := f.Seek(pos, io.SeekStart); err != nil {
			f.Close()
			return out, cur, err
		}
		rd := bufio.NewReader(f)
		for len(out) < drainBatchLines && size < drainBatchBytes {
			line, err := rd.ReadBytes('\n')
			if err != nil {
				break // EOF; a partial last line is left for the next read
			}
			pos += int64(len(line))
			if e, ok := parseDrainLine(app, line); ok {
				out = append(out, e)
				size += len(e.Message) + 128
			}
		}
		f.Close()
		cur = drainCursor{Day: day, Pos: pos}
		if len(out) >= drainBatchLines || size >= drainBatchBytes || i == len(days)-1 {
			break
		}
	}
	return out, cur, nil
}

func parseDrainLine(app string, line []byte) (drainEntry, bool) {
	parts := strings.SplitN(strings.TrimRight(string(line), "\n"), "\t", 4)
	if len(parts) != 4 {
		return drainEntry{}, false
	}
	msg := strings.ToValidUTF8(parts[3], "�")
	if len(msg) > drainMessageBytes {
		msg = strings.ToValidUTF8(msg[:drainMessageBytes], "")
	}
	return drainEntry{Timestamp: parts[0], Message: msg, App: app, Stream: parts[2], Container: parts[1]}, true
}

func drainsAllowPrivate() bool { return os.Getenv("SKIFF_ALLOW_PRIVATE_ENDPOINTS") == "1" }

// newDrainClient refuses redirects so the auth header can't follow one to another host.
func newDrainClient() *http.Client {
	c := publicHTTPClient(15 * time.Second)
	if drainsAllowPrivate() {
		c = &http.Client{Timeout: 15 * time.Second}
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return c
}

var drainClient = sync.OnceValue(newDrainClient)

func postDrain(d LogDrain, entries []drainEntry) error {
	body, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, d.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if d.HeaderName != "" {
		req.Header.Set(d.HeaderName, d.HeaderValue)
	}
	resp, err := drainClient().Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			return ue.Err // the URL can carry a key; don't repeat it in the status
		}
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("the endpoint answered %d", resp.StatusCode)
	}
	return nil
}

// shipLogs sends what's waiting for each of the team's apps and reports whether anything went out; it stops at the first failed send, which is retried from the same place.
func shipLogs(d LogDrain) (sent bool, err error) {
	for _, app := range teamApps(d.Team) {
		cur := getDrainCursor(d.Team, app)
		for range drainPassBatches {
			entries, next, err := readDrainBatch(app, cur)
			if err != nil {
				return sent, err
			}
			if len(entries) > 0 {
				if err := postDrain(d, entries); err != nil {
					return sent, err
				}
				sent = true
			}
			if next != cur {
				setDrainCursor(d.Team, app, next)
			}
			caughtUp := next == cur || (len(entries) < drainBatchLines && next.Day >= lastDay(app))
			cur = next
			if caughtUp {
				break
			}
		}
	}
	return sent, nil
}

func lastDay(app string) string {
	days := logDays(app)
	if len(days) == 0 {
		return ""
	}
	return days[len(days)-1]
}

type drainState struct {
	running bool
	fails   int
	next    time.Time
}

var (
	drainStatesMu sync.Mutex
	drainStates   = map[string]*drainState{}
)

func drainBackoff(fails int) time.Duration {
	if fails > 10 {
		return drainMaxBackoff
	}
	if d := drainTick << fails; d < drainMaxBackoff {
		return d
	}
	return drainMaxBackoff
}

// runDrains starts a pass for every drain that isn't mid-pass or waiting out a failure; each runs on its own, so one slow endpoint can't hold up another team's.
func runDrains(now time.Time) {
	for _, d := range allLogDrains() {
		drainStatesMu.Lock()
		st := drainStates[d.Team]
		if st == nil {
			st = &drainState{}
			drainStates[d.Team] = st
		}
		if st.running || now.Before(st.next) {
			drainStatesMu.Unlock()
			continue
		}
		st.running = true
		drainStatesMu.Unlock()
		go func(d LogDrain) {
			var sent bool
			var err error
			guard("logDrain", func() { sent, err = shipLogs(d) })
			noteDrainResult(d.Team, sent, err)
			drainStatesMu.Lock()
			defer drainStatesMu.Unlock()
			st.running = false
			if err != nil {
				st.next = time.Now().Add(drainBackoff(st.fails))
				st.fails++
			} else {
				st.fails, st.next = 0, time.Time{}
			}
		}(d)
	}
}

func clearDrainBackoff(team string) {
	drainStatesMu.Lock()
	defer drainStatesMu.Unlock()
	if st := drainStates[team]; st != nil {
		st.fails, st.next = 0, time.Time{}
	}
}

func (p *Panel) logDrainLoop() {
	for {
		guard("logDrains", func() { runDrains(time.Now()) })
		time.Sleep(drainTick)
	}
}

var (
	drainHeaderName = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)
	drainHeaderDeny = map[string]bool{"host": true, "content-length": true, "content-type": true, "transfer-encoding": true, "connection": true}
)

// validateDrain checks a drain's address and header without contacting it.
func validateDrain(d LogDrain) error {
	u, err := url.Parse(d.URL)
	if err != nil || u.Host == "" || len(d.URL) > 2000 || (u.Scheme != "https" && u.Scheme != "http") {
		return errors.New("enter the full URL your log service gave you, starting with https://")
	}
	if u.Scheme != "https" && !drainsAllowPrivate() {
		return errors.New("the URL must start with https://, since logs and the header are sent to it")
	}
	if d.HeaderName == "" {
		return nil
	}
	if !drainHeaderName.MatchString(d.HeaderName) || drainHeaderDeny[strings.ToLower(d.HeaderName)] {
		return fmt.Errorf("%q can't be used as the header name", d.HeaderName)
	}
	if d.HeaderValue == "" {
		return errors.New("enter the header's value")
	}
	if strings.ContainsAny(d.HeaderValue, "\r\n") {
		return errors.New("the header's value can't span lines")
	}
	return nil
}

// handleLogDrain manages the team's drain. Owner only, reads included: it decides where all the team's logs go, and its URL can hold a key.
func (p *Panel) handleLogDrain(w http.ResponseWriter, r *http.Request) {
	team := p.teamID(r)
	if team == "" || !p.isOwner(r) {
		http.Error(w, "only team owners can manage the log drain", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
		d, ok := getLogDrain(team)
		writeJSON(w, map[string]any{
			"configured": ok, "url": d.URL, "headerName": d.HeaderName, "headerSet": d.HeaderValue != "",
			"lastOk": d.LastOK, "lastError": d.LastError,
		})
	case http.MethodPut:
		var body struct {
			URL         string `json:"url"`
			HeaderName  string `json:"headerName"`
			HeaderValue string `json:"headerValue"`
		}
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body)
		d, _ := getLogDrain(team)
		prevName, prevValue := d.HeaderName, d.HeaderValue
		d.Team, d.URL, d.HeaderName, d.HeaderValue = team, strings.TrimSpace(body.URL), strings.TrimSpace(body.HeaderName), strings.TrimSpace(body.HeaderValue)
		// A blank value keeps the saved one, unless it would now go somewhere else.
		if d.HeaderValue == "" && d.HeaderName != "" && d.HeaderName == prevName && sameDrainHost(d.URL, team) {
			d.HeaderValue = prevValue
		}
		if err := validateDrain(d); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := saveLogDrain(d); err != nil {
			http.Error(w, "couldn't save the log drain", http.StatusInternalServerError)
			return
		}
		clearDrainBackoff(team)
		p.audit(r, "logdrain.update", "", "")
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		deleteLogDrain(team)
		p.audit(r, "logdrain.delete", "", "")
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func sameDrainHost(newURL, team string) bool {
	d, ok := getLogDrain(team)
	if !ok {
		return false
	}
	a, errA := url.Parse(newURL)
	b, errB := url.Parse(d.URL)
	return errA == nil && errB == nil && a.Host == b.Host
}

// handleLogDrainTest sends one line to the saved drain and reports what came back.
func (p *Panel) handleLogDrainTest(w http.ResponseWriter, r *http.Request) {
	team := p.teamID(r)
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if team == "" || !p.isOwner(r) {
		http.Error(w, "only team owners can manage the log drain", http.StatusForbidden)
		return
	}
	d, ok := getLogDrain(team)
	if !ok {
		http.Error(w, "save the log drain first", http.StatusBadRequest)
		return
	}
	err := postDrain(d, []drainEntry{{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Message:   "Test line from Skiff: your log drain is connected.",
		App:       "skiff-test", Stream: "out", Container: "skiff",
	}})
	if err != nil {
		http.Error(w, "the test line didn't arrive: "+err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
