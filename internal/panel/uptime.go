package panel

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nuelScript/skiff/internal/registry"
)

const (
	uptimeEvery       = time.Minute
	uptimeKeep        = 30 * 24 * time.Hour
	uptimeFailsToOpen = 2
)

type probeResult struct {
	OK   bool
	Code int
	Ms   int64
	Err  string
}

var uptimeClient = &http.Client{
	Timeout: 10 * time.Second,
	// A redirect is a healthy answer; following it could leave the app.
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	Transport:     &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, DisableKeepAlives: true},
}

// probe asks the app for its home page; anything below a 5xx counts as up, since a 404 at / is still an app answering.
func probe(url string) probeResult {
	start := time.Now()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return probeResult{Err: err.Error()}
	}
	req.Header.Set("User-Agent", "skiff-uptime/1 (monitor)")
	resp, err := uptimeClient.Do(req)
	ms := time.Since(start).Milliseconds()
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "Client.Timeout") {
			msg = "timed out after 10s"
		}
		return probeResult{Ms: ms, Err: msg}
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	return probeResult{OK: resp.StatusCode < 500, Code: resp.StatusCode, Ms: ms}
}

func (p *Panel) probeURL(a registry.App) string {
	src, _ := getSource(a.Name)
	if !src.Private && p.domain != "" && p.domain != "localhost" && strings.Contains(p.domain, ".") {
		return "https://" + a.Name + "." + p.domain + "/"
	}
	port := a.HostPort
	if live, err := p.eng.HostPort(a.Container, a.Port); err == nil {
		port = live
	}
	return fmt.Sprintf("http://127.0.0.1:%d/", port)
}

type uptimeTracker struct {
	mu    sync.Mutex
	fails map[string]int
	open  map[string]int64 // app → open incident id
}

var uptime = &uptimeTracker{fails: map[string]int{}, open: map[string]int64{}}

func (u *uptimeTracker) loadOpen() {
	rows, err := sqlDB.Query(`SELECT id, app FROM incidents WHERE ended=0`)
	if err != nil {
		return
	}
	defer rows.Close()
	u.mu.Lock()
	defer u.mu.Unlock()
	for rows.Next() {
		var id int64
		var app string
		if rows.Scan(&id, &app) == nil {
			u.open[app] = id
		}
	}
}

// observe records one check and opens or closes the app's incident, returning what changed so the caller can alert.
func (u *uptimeTracker) observe(app, team string, r probeResult, now time.Time) (opened, closed bool, downFor time.Duration) {
	_, _ = sqlDB.Exec(`INSERT INTO uptime_checks(app,t,ok,code,ms) VALUES(?,?,?,?,?)`, app, now.Unix(), b2i(r.OK), r.Code, r.Ms)
	u.mu.Lock()
	defer u.mu.Unlock()
	id, isOpen := u.open[app]
	if r.OK {
		u.fails[app] = 0
		if isOpen {
			var started int64
			_ = sqlDB.QueryRow(`SELECT started FROM incidents WHERE id=?`, id).Scan(&started)
			_, _ = sqlDB.Exec(`UPDATE incidents SET ended=? WHERE id=?`, now.Unix(), id)
			delete(u.open, app)
			return false, true, now.Sub(time.Unix(started, 0))
		}
		return false, false, 0
	}
	u.fails[app]++
	if u.fails[app] < uptimeFailsToOpen || isOpen {
		return false, false, 0
	}
	cause := r.Err
	if cause == "" {
		cause = fmt.Sprintf("HTTP %d", r.Code)
	}
	started := now.Add(-time.Duration(uptimeFailsToOpen-1) * uptimeEvery).Unix()
	res, err := sqlDB.Exec(`INSERT INTO incidents(app,team,started,ended,cause) VALUES(?,?,?,0,?)`, app, team, started, cause)
	if err == nil {
		u.open[app], _ = res.LastInsertId()
	}
	return true, false, 0
}

func (p *Panel) checkUptime() {
	apps, err := registry.Load()
	if err != nil {
		return
	}
	var wg sync.WaitGroup
	for name, a := range apps {
		src, ok := getSource(name)
		if !ok || !uptimeWatched(src, a) || isInflight(name) || isSleeping(name) {
			continue
		}
		wg.Add(1)
		go func(a registry.App, team string) {
			defer wg.Done()
			r := probe(p.probeURL(a))
			opened, closed, downFor := uptime.observe(a.Name, team, r, time.Now())
			switch {
			case opened:
				cause := r.Err
				if cause == "" {
					cause = fmt.Sprintf("it answered HTTP %d", r.Code)
				}
				dispatchAlert(alertEvent{Team: team, Kind: "app.down", App: a.Name,
					Title: "Down: " + a.Name, Detail: "Its URL stopped answering: " + cause + "."})
			case closed:
				dispatchAlert(alertEvent{Team: team, Kind: "app.up", App: a.Name,
					Title: "Back up: " + a.Name, Detail: "Answering again after " + roundDur(downFor) + "."})
			}
		}(a, src.Team)
	}
	wg.Wait()
	_, _ = sqlDB.Exec(`DELETE FROM uptime_checks WHERE t < ?`, time.Now().Add(-uptimeKeep).Unix())
}

// uptimeWatched says which apps the uptime probe covers; container-health alerts handle the rest, so one outage alerts once.
func uptimeWatched(src Source, a registry.App) bool {
	return src.Parent == "" && a.Host == ""
}

func roundDur(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "under a minute"
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	default:
		return fmt.Sprintf("%.1f h", d.Hours())
	}
}

func (p *Panel) uptimeLoop() {
	uptime.loadOpen()
	time.Sleep(90 * time.Second)
	tick := time.NewTicker(uptimeEvery)
	defer tick.Stop()
	for {
		guard("checkUptime", p.checkUptime)
		<-tick.C
	}
}

type uptimeDay struct {
	Day    int64   `json:"day"` // unix, start of day UTC
	Pct    float64 `json:"pct"` // -1 = no checks
	Checks int     `json:"checks"`
}

type incident struct {
	ID      int64  `json:"id"`
	Started int64  `json:"started"`
	Ended   int64  `json:"ended"`
	Cause   string `json:"cause"`
}

type uptimeView struct {
	Up        bool        `json:"up"`
	Checked   int64       `json:"checked"`
	Code      int         `json:"code"`
	Ms        int64       `json:"ms"`
	Pct24h    float64     `json:"pct24h"`
	Pct7d     float64     `json:"pct7d"`
	Pct30d    float64     `json:"pct30d"`
	Days      []uptimeDay `json:"days"`
	Incidents []incident  `json:"incidents"`
}

func uptimePct(app string, since time.Time) float64 {
	var total, ok int
	_ = sqlDB.QueryRow(`SELECT COUNT(*), COALESCE(SUM(ok),0) FROM uptime_checks WHERE app=? AND t>=?`, app, since.Unix()).Scan(&total, &ok)
	if total == 0 {
		return -1
	}
	return float64(ok) * 100 / float64(total)
}

func buildUptime(app string, now time.Time) uptimeView {
	v := uptimeView{Days: []uptimeDay{}, Incidents: []incident{}, Checked: 0}
	var ok int
	if sqlDB.QueryRow(`SELECT t, ok, code, ms FROM uptime_checks WHERE app=? ORDER BY t DESC LIMIT 1`, app).
		Scan(&v.Checked, &ok, &v.Code, &v.Ms) == nil {
		v.Up = ok == 1
	}
	v.Pct24h = uptimePct(app, now.Add(-24*time.Hour))
	v.Pct7d = uptimePct(app, now.Add(-7*24*time.Hour))
	v.Pct30d = uptimePct(app, now.Add(-30*24*time.Hour))

	today := now.UTC().Truncate(24 * time.Hour)
	first := today.AddDate(0, 0, -29)
	byDay := map[int64][2]int{}
	rows, err := sqlDB.Query(`SELECT t, ok FROM uptime_checks WHERE app=? AND t>=?`, app, first.Unix())
	if err == nil {
		for rows.Next() {
			var t int64
			var k int
			if rows.Scan(&t, &k) == nil {
				d := t / 86400 * 86400
				c := byDay[d]
				c[0]++
				c[1] += k
				byDay[d] = c
			}
		}
		rows.Close()
	}
	for d := first; !d.After(today); d = d.AddDate(0, 0, 1) {
		c := byDay[d.Unix()]
		day := uptimeDay{Day: d.Unix(), Pct: -1, Checks: c[0]}
		if c[0] > 0 {
			day.Pct = float64(c[1]) * 100 / float64(c[0])
		}
		v.Days = append(v.Days, day)
	}

	irows, err := sqlDB.Query(`SELECT id, started, ended, cause FROM incidents WHERE app=? ORDER BY started DESC LIMIT 10`, app)
	if err == nil {
		for irows.Next() {
			var in incident
			if irows.Scan(&in.ID, &in.Started, &in.Ended, &in.Cause) == nil {
				v.Incidents = append(v.Incidents, in)
			}
		}
		irows.Close()
	}
	return v
}

func (p *Panel) handleUptime(w http.ResponseWriter, r *http.Request) {
	app := sanitizeName(r.URL.Query().Get("app"))
	if !p.canAccess(r, app) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	writeJSON(w, buildUptime(app, time.Now()))
}
