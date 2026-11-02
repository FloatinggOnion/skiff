package panel

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	requestsKeep = 7 * 24 * time.Hour
	// requestsMaxRows bounds per-request detail on a busy server, where seven days of it could fill the disk: past this, the oldest goes first. Totals in Analytics come from the router and aren't affected.
	requestsMaxRows = 3_000_000
	ingestBatch     = 2 << 20
	pruneBatch      = 20_000
)

type reqLine struct {
	T  int64  `json:"t"`
	A  string `json:"a"`
	M  string `json:"m"`
	P  string `json:"p"`
	S  int    `json:"s"`
	D  int64  `json:"d"`
	B  int64  `json:"b"`
	R  string `json:"r"`
	V  string `json:"v"`
	Bt bool   `json:"bt"`
}

func requestsDir() string { return filepath.Join(skiffDir(), "requests") }

// ingestRequests loads whatever the router has appended to its daily request logs since the last pass.
func ingestRequests(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, nil
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jsonl") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	total := 0
	for _, name := range names {
		for {
			n, err := ingestFile(filepath.Join(dir, name), name)
			total += n
			if err != nil {
				return total, err
			}
			if n == 0 {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	return total, nil
}

func ingestFile(path, name string) (int, error) {
	var offset int64
	_ = sqlDB.QueryRow(`SELECT pos FROM request_ingest WHERE file=?`, name).Scan(&offset)
	f, err := os.Open(path)
	if err != nil {
		return 0, nil
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || info.Size() <= offset {
		return 0, nil
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return 0, err
	}
	// Small batches: the database has one connection, so a long transaction would freeze the dashboard for its whole length.
	data, err := io.ReadAll(io.LimitReader(f, ingestBatch))
	if err != nil {
		return 0, err
	}
	// Only whole lines: the router may be mid-write on the last one.
	end := bytes.LastIndexByte(data, '\n')
	if end < 0 {
		return 0, nil
	}
	data = data[:end+1]

	tx, err := sqlDB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck
	stmt, err := tx.Prepare(`INSERT INTO requests(t,app,method,path,status,dur,bytes,ref,visitor,bot) VALUES(?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	n := 0
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		var e reqLine
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.A == "" {
			continue
		}
		if _, err := stmt.Exec(e.T, e.A, e.M, e.P, e.S, e.D, e.B, e.R, e.V, b2i(e.Bt)); err != nil {
			return 0, err
		}
		n++
	}
	if _, err := tx.Exec(`INSERT INTO request_ingest(file,pos) VALUES(?,?)
		ON CONFLICT(file) DO UPDATE SET pos=excluded.pos`, name, offset+int64(len(data))); err != nil {
		return 0, err
	}
	return n, tx.Commit()
}

func pruneRequests(now time.Time) {
	cutoff := now.Add(-requestsKeep).UnixMilli()
	var byCount int64
	if sqlDB.QueryRow(`SELECT t FROM requests ORDER BY t DESC LIMIT 1 OFFSET ?`, requestsMaxRows).Scan(&byCount) == nil && byCount > cutoff {
		cutoff = byCount
	}
	// In small deletes with a gap between, for the same reason ingest is batched.
	for {
		res, err := sqlDB.Exec(`DELETE FROM requests WHERE rowid IN (SELECT rowid FROM requests WHERE t < ? LIMIT ?)`, cutoff, pruneBatch)
		if err != nil {
			break
		}
		if n, _ := res.RowsAffected(); n < pruneBatch {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	_, _ = sqlDB.Exec(`DELETE FROM request_ingest WHERE file < ?`, now.Add(-requestsKeep).UTC().Format("2006-01-02")+".jsonl")
}

func (p *Panel) requestsLoop() {
	last := time.Time{}
	for {
		guard("ingestRequests", func() {
			if _, err := ingestRequests(requestsDir()); err != nil {
				log.Printf("skiff: reading request logs: %v", err)
			}
			if time.Since(last) > time.Hour {
				pruneRequests(time.Now())
				last = time.Now()
			}
		})
		time.Sleep(10 * time.Second)
	}
}

type insightPoint struct {
	T        int64 `json:"t"`
	Req      int   `json:"req"`
	E4       int   `json:"e4"`
	E5       int   `json:"e5"`
	Lat      int64 `json:"lat"` // average ms
	Visitors int   `json:"visitors"`
}

type insightRow struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
	Extra int64  `json:"extra,omitempty"`
}

type insightMarker struct {
	ID      string `json:"id"`
	T       int64  `json:"t"`
	Commit  string `json:"commit"`
	Message string `json:"message"`
	Trigger string `json:"trigger"`
	Status  string `json:"status"`
}

type insightsResponse struct {
	Range      string          `json:"range"`
	BucketSecs int64           `json:"bucketSecs"`
	From       int64           `json:"from"`
	Requests   int             `json:"requests"`
	Errors4    int             `json:"errors4"`
	Errors5    int             `json:"errors5"`
	P50        int64           `json:"p50"`
	P95        int64           `json:"p95"`
	P99        int64           `json:"p99"`
	Visitors   int             `json:"visitors"`
	Pageviews  int             `json:"pageviews"`
	Bots       int             `json:"bots"`
	Bytes      int64           `json:"bytes"`
	Series     []insightPoint  `json:"series"`
	Paths      []insightRow    `json:"paths"`
	Pages      []insightRow    `json:"pages"`
	ErrorPaths []insightRow    `json:"errorPaths"`
	Referrers  []insightRow    `json:"referrers"`
	Deploys    []insightMarker `json:"deploys"`
}

var insightRanges = map[string]struct{ span, bucket time.Duration }{
	"1h":  {time.Hour, time.Minute},
	"24h": {24 * time.Hour, 15 * time.Minute},
	"7d":  {7 * 24 * time.Hour, 2 * time.Hour},
}

var assetExts = map[string]bool{
	".js": true, ".css": true, ".map": true, ".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".svg": true, ".webp": true, ".avif": true, ".ico": true, ".woff": true, ".woff2": true, ".ttf": true,
	".txt": true, ".xml": true, ".json": true, ".webmanifest": true, ".mp4": true, ".webm": true,
}

// isPage tells a page load apart from the assets and API calls it pulls in.
func isPage(p string) bool {
	if strings.HasPrefix(p, "/_next/") || strings.HasPrefix(p, "/assets/") || strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/static/") {
		return false
	}
	return !assetExts[strings.ToLower(path.Ext(p))]
}

func buildInsights(app, rng string, now time.Time) (insightsResponse, error) {
	cfg, ok := insightRanges[rng]
	if !ok {
		rng, cfg = "24h", insightRanges["24h"]
	}
	bucket := int64(cfg.bucket / time.Millisecond)
	from := now.Add(-cfg.span).UnixMilli()
	from = from / bucket * bucket
	out := insightsResponse{
		Range: rng, BucketSecs: bucket / 1000, From: from / 1000,
		Series: []insightPoint{}, Paths: []insightRow{}, Pages: []insightRow{},
		ErrorPaths: []insightRow{}, Referrers: []insightRow{}, Deploys: []insightMarker{},
	}

	rows, err := sqlDB.Query(`SELECT t, path, status, dur, bytes, ref, visitor, bot, method FROM requests WHERE app=? AND t>=? ORDER BY t`, app, from)
	if err != nil {
		return out, err
	}
	defer rows.Close()

	points := map[int64]*insightPoint{}
	pointVisitors := map[int64]map[string]bool{}
	visitors := map[string]bool{}
	paths := map[string]*insightRow{}
	pages := map[string]int{}
	errPaths := map[string]int{}
	refVisitors := map[string]map[string]bool{}
	var durs []int64
	for rows.Next() {
		var (
			t, dur, size            int64
			p, ref, visitor, method string
			status, bot             int
		)
		if rows.Scan(&t, &p, &status, &dur, &size, &ref, &visitor, &bot, &method) != nil {
			continue
		}
		bk := t / bucket * bucket
		pt := points[bk]
		if pt == nil {
			pt = &insightPoint{T: bk / 1000}
			points[bk] = pt
			pointVisitors[bk] = map[string]bool{}
		}
		pt.Req++
		pt.Lat += dur
		out.Requests++
		out.Bytes += size
		durs = append(durs, dur)
		switch {
		case status >= 500:
			pt.E5++
			out.Errors5++
			errPaths[p+" "+strconv.Itoa(status)]++
		case status >= 400:
			pt.E4++
			out.Errors4++
			if bot == 0 {
				errPaths[p+" "+strconv.Itoa(status)]++
			}
		}
		if bot != 0 {
			out.Bots++
			continue
		}
		r := paths[p]
		if r == nil {
			r = &insightRow{Key: p}
			paths[p] = r
		}
		r.Count++
		r.Extra += dur
		visitors[visitor] = true
		pointVisitors[bk][visitor] = true
		if method == http.MethodGet && status < 400 && isPage(p) {
			out.Pageviews++
			pages[p]++
		}
		if ref != "" {
			if refVisitors[ref] == nil {
				refVisitors[ref] = map[string]bool{}
			}
			refVisitors[ref][visitor] = true
		}
	}
	if err := rows.Err(); err != nil {
		return out, err
	}

	for t := from; t <= now.UnixMilli(); t += bucket {
		pt := points[t]
		if pt == nil {
			out.Series = append(out.Series, insightPoint{T: t / 1000})
			continue
		}
		pt.Lat /= int64(pt.Req)
		pt.Visitors = len(pointVisitors[t])
		out.Series = append(out.Series, *pt)
	}
	out.Visitors = len(visitors)
	out.P50, out.P95, out.P99 = percentile(durs, 50), percentile(durs, 95), percentile(durs, 99)

	for _, r := range paths {
		r.Extra /= int64(r.Count)
		out.Paths = append(out.Paths, *r)
	}
	out.Paths = topRows(out.Paths, 10)
	out.Pages = topRows(countRows(pages), 10)
	out.ErrorPaths = topRows(countRows(errPaths), 10)
	refs := map[string]int{}
	for host, vs := range refVisitors {
		refs[host] = len(vs)
	}
	out.Referrers = topRows(countRows(refs), 10)
	out.Deploys = deployMarkers(app, from/1000)
	return out, nil
}

func deployMarkers(app string, from int64) []insightMarker {
	rows, err := sqlDB.Query(`SELECT id, started, commit_sha, message, trigger, status FROM deploys
		WHERE app=? AND started>=? ORDER BY started`, app, from)
	if err != nil {
		return []insightMarker{}
	}
	defer rows.Close()
	out := []insightMarker{}
	for rows.Next() {
		var m insightMarker
		if rows.Scan(&m.ID, &m.T, &m.Commit, &m.Message, &m.Trigger, &m.Status) == nil {
			out = append(out, m)
		}
	}
	return out
}

func countRows(m map[string]int) []insightRow {
	out := make([]insightRow, 0, len(m))
	for k, n := range m {
		out = append(out, insightRow{Key: k, Count: n})
	}
	return out
}

func topRows(rows []insightRow, n int) []insightRow {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count != rows[j].Count {
			return rows[i].Count > rows[j].Count
		}
		return rows[i].Key < rows[j].Key
	})
	if len(rows) > n {
		rows = rows[:n]
	}
	return rows
}

func percentile(vals []int64, p int) int64 {
	if len(vals) == 0 {
		return 0
	}
	s := append([]int64(nil), vals...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	i := (len(s)*p + 99) / 100
	if i < 1 {
		i = 1
	}
	return s[i-1]
}

func (p *Panel) handleInsights(w http.ResponseWriter, r *http.Request) {
	app := sanitizeName(r.URL.Query().Get("app"))
	if !p.canAccess(r, app) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	out, err := buildInsights(app, r.URL.Query().Get("range"), time.Now())
	if err != nil && err != sql.ErrNoRows {
		http.Error(w, "couldn't read requests", http.StatusInternalServerError)
		return
	}
	writeJSON(w, out)
}
