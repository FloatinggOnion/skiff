package panel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// StatusPage is a team's public page: nothing is public until it's enabled, and only the projects it lists appear.
type StatusPage struct {
	Team    string   `json:"-"`
	Slug    string   `json:"slug"`
	Enabled bool     `json:"enabled"`
	Title   string   `json:"title"`
	Host    string   `json:"host"`
	Apps    []string `json:"apps"`
}

func getStatusPage(team string) (StatusPage, bool) {
	return scanStatusPage(sqlDB.QueryRow(`SELECT team,slug,enabled,title,host,apps FROM status_pages WHERE team=?`, team))
}

func statusPageBySlug(slug string) (StatusPage, bool) {
	return scanStatusPage(sqlDB.QueryRow(`SELECT team,slug,enabled,title,host,apps FROM status_pages WHERE slug=?`, slug))
}

func scanStatusPage(row interface{ Scan(...any) error }) (StatusPage, bool) {
	var s StatusPage
	var enabled int
	var apps string
	if row.Scan(&s.Team, &s.Slug, &enabled, &s.Title, &s.Host, &apps) != nil {
		return StatusPage{}, false
	}
	s.Enabled = enabled != 0
	_ = json.Unmarshal([]byte(apps), &s.Apps)
	if s.Apps == nil {
		s.Apps = []string{}
	}
	return s, true
}

func saveStatusPage(s StatusPage) error {
	apps, _ := json.Marshal(s.Apps)
	_, err := sqlDB.Exec(`INSERT INTO status_pages(team,slug,enabled,title,host,apps) VALUES(?,?,?,?,?,?)
		ON CONFLICT(team) DO UPDATE SET enabled=excluded.enabled, title=excluded.title, host=excluded.host, apps=excluded.apps`,
		s.Team, s.Slug, b2i(s.Enabled), s.Title, s.Host, string(apps))
	return err
}

func deleteStatusPage(team string) {
	_, _ = sqlDB.Exec(`DELETE FROM status_pages WHERE team=?`, team)
	writeDomainsFile()
}

// statusDomains maps the custom domains of enabled pages to their slugs.
func statusDomains() map[string]string {
	out := map[string]string{}
	rows, err := sqlDB.Query(`SELECT host, slug FROM status_pages WHERE enabled=1 AND host<>''`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var host, slug string
		if rows.Scan(&host, &slug) == nil {
			out[host] = slug
		}
	}
	return out
}

var statusSlugRe = regexp.MustCompile(`[^a-z0-9-]+`)

// newStatusSlug starts from the team's name; it stays put afterwards, so a renamed team doesn't break a shared link.
func newStatusSlug(teamName string) string {
	base := strings.Trim(statusSlugRe.ReplaceAllString(strings.ToLower(teamName), "-"), "-")
	if base == "" {
		base = "status"
	}
	slug := base
	for {
		if _, taken := statusPageBySlug(slug); !taken {
			return slug
		}
		slug = base + "-" + randHex(2)
	}
}

// handleStatusPage configures the team's page. Owner only: it decides what the team shows the public.
func (p *Panel) handleStatusPage(w http.ResponseWriter, r *http.Request) {
	team := p.teamID(r)
	if team == "" || !p.isOwner(r) {
		http.Error(w, "only team owners can manage the status page", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s, ok := getStatusPage(team)
		if !ok {
			s = StatusPage{Apps: []string{}}
		}
		writeJSON(w, map[string]any{
			"enabled": s.Enabled, "title": s.Title, "host": s.Host, "apps": s.Apps,
			"url": "https://status." + p.domain + "/" + s.Slug, "slug": s.Slug,
		})
	case http.MethodPut:
		var body struct {
			Enabled bool     `json:"enabled"`
			Title   string   `json:"title"`
			Host    string   `json:"host"`
			Apps    []string `json:"apps"`
		}
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body)
		s, ok := getStatusPage(team)
		if !ok {
			name := ""
			if sess, signedIn := p.session(r); signedIn {
				for _, t := range p.auth.TeamsForUser(sess.userID) {
					if t.ID == team {
						name = t.Name
					}
				}
			}
			s = StatusPage{Team: team, Slug: newStatusSlug(name)}
		}
		s.Enabled = body.Enabled
		s.Title = strings.TrimSpace(body.Title)
		if len(s.Title) > 80 {
			http.Error(w, "keep the title under 80 characters", http.StatusBadRequest)
			return
		}
		s.Host = ""
		if h := strings.TrimSpace(body.Host); h != "" {
			host, valid := normalizeHost(h, p.domain)
			if !valid {
				http.Error(w, "enter a domain like status.example.com", http.StatusBadRequest)
				return
			}
			if _, taken := domainOwner(host); taken {
				http.Error(w, "that domain is already pointed at a project", http.StatusConflict)
				return
			}
			if owner, used := statusDomains()[host]; used && owner != s.Slug {
				http.Error(w, "that domain is already used by another status page", http.StatusConflict)
				return
			}
			if host != s.Host {
				if problem := p.proveOwnership(r, host); problem != "" {
					http.Error(w, problem, http.StatusUnprocessableEntity)
					return
				}
			}
			s.Host = host
		}
		// Only this team's own projects, and never a preview.
		s.Apps = []string{}
		for _, app := range body.Apps {
			if src, ok := getSource(sanitizeName(app)); ok && src.Team == team && src.Parent == "" {
				s.Apps = append(s.Apps, src.App)
			}
		}
		sort.Strings(s.Apps)
		if err := saveStatusPage(s); err != nil {
			http.Error(w, "couldn't save the status page", http.StatusInternalServerError)
			return
		}
		writeDomainsFile()
		publicStatusCache.clear(s.Slug)
		p.audit(r, "statuspage.update", "", "")
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type statusBar struct {
	Class string
	Label string
}

type statusRow struct {
	Name  string
	Class string // op | down | idle
	Label string
	Pct   string
	Bars  []statusBar
}

type statusEvent struct {
	App      string
	When     string
	Duration string
}

type statusData struct {
	Title    string
	Class    string // op | partial | major | empty
	Headline string
	Rows     []statusRow
	Events   []statusEvent
	Updated  string
}

const statusStale = 10 * time.Minute

// buildStatusData is everything the public sees. Incident causes are left out: they can name internal hosts and errors.
func buildStatusData(s StatusPage, now time.Time) statusData {
	d := statusData{Title: s.Title, Updated: now.UTC().Format("15:04 UTC")}
	if d.Title == "" {
		d.Title = "Status"
	}
	up, down := 0, 0
	for _, app := range s.Apps {
		src, ok := getSource(app)
		if !ok || src.Team != s.Team || src.Parent != "" {
			continue // deleted, or the name now belongs to someone else
		}
		v := buildUptime(app, now)
		row := statusRow{Name: app}
		switch {
		case isSleeping(app) || (v.Up && now.Unix()-v.Checked < int64(statusStale.Seconds())):
			row.Class, row.Label = "op", "Operational"
			up++
		case v.Checked == 0 || now.Unix()-v.Checked >= int64(statusStale.Seconds()):
			row.Class, row.Label = "idle", "No recent data"
		default:
			row.Class, row.Label = "down", "Down"
			down++
		}
		if v.Pct30d >= 0 {
			row.Pct = fmt.Sprintf("%.2f%% uptime", v.Pct30d)
		}
		for _, day := range v.Days {
			date := time.Unix(day.Day, 0).UTC().Format("Jan 2")
			switch {
			case day.Pct < 0:
				row.Bars = append(row.Bars, statusBar{"none", date + ": no data"})
			case day.Pct >= 99.9:
				row.Bars = append(row.Bars, statusBar{"good", fmt.Sprintf("%s: %.2f%%", date, day.Pct)})
			case day.Pct >= 95:
				row.Bars = append(row.Bars, statusBar{"warn", fmt.Sprintf("%s: %.2f%%", date, day.Pct)})
			default:
				row.Bars = append(row.Bars, statusBar{"bad", fmt.Sprintf("%s: %.2f%%", date, day.Pct)})
			}
		}
		d.Rows = append(d.Rows, row)
		for _, in := range v.Incidents {
			if now.Unix()-in.Started > int64(30*24*3600) {
				continue
			}
			ev := statusEvent{App: app, When: time.Unix(in.Started, 0).UTC().Format("Jan 2, 15:04 UTC"), Duration: "ongoing"}
			if in.Ended > 0 {
				ev.Duration = "down for " + roundDur(time.Duration(in.Ended-in.Started)*time.Second)
			}
			d.Events = append(d.Events, ev)
		}
	}
	sort.SliceStable(d.Events, func(i, j int) bool { return d.Events[i].When > d.Events[j].When })
	if len(d.Events) > 8 {
		d.Events = d.Events[:8]
	}
	switch {
	case len(d.Rows) == 0:
		d.Class, d.Headline = "empty", "Nothing listed yet"
	case down == 0 && up == len(d.Rows):
		d.Class, d.Headline = "op", "All systems operational"
	case down == 0:
		d.Class, d.Headline = "empty", "Waiting for recent checks"
	case up == 0:
		d.Class, d.Headline = "major", "Major outage"
	default:
		d.Class, d.Headline = "partial", "Partial outage"
	}
	return d
}

const publicStatusTTL = 30 * time.Second

type statusCacheEntry struct {
	body []byte
	at   time.Time
}

// publicStatusCache stops public traffic from turning into database load: each page renders at most once per TTL.
var publicStatusCache = &statusCache{entries: map[string]statusCacheEntry{}}

type statusCache struct {
	sync.Mutex
	entries map[string]statusCacheEntry
}

func (c *statusCache) clear(slug string) {
	c.Lock()
	delete(c.entries, slug)
	c.Unlock()
}

func (c *statusCache) render(s StatusPage, now time.Time) []byte {
	c.Lock()
	defer c.Unlock()
	if e, ok := c.entries[s.Slug]; ok && now.Sub(e.at) < publicStatusTTL {
		return e.body
	}
	var b bytes.Buffer
	_ = statusTmpl.Execute(&b, buildStatusData(s, now))
	c.entries[s.Slug] = statusCacheEntry{b.Bytes(), now}
	return b.Bytes()
}

// onlyStatusPage is the page to show at the bare address: it's unambiguous only when one team has a page.
func onlyStatusPage() (StatusPage, bool) {
	rows, err := sqlDB.Query(`SELECT team,slug,enabled,title,host,apps FROM status_pages WHERE enabled=1 LIMIT 2`)
	if err != nil {
		return StatusPage{}, false
	}
	defer rows.Close()
	var pages []StatusPage
	for rows.Next() {
		if s, ok := scanStatusPage(rows); ok {
			pages = append(pages, s)
		}
	}
	if len(pages) != 1 {
		return StatusPage{}, false
	}
	return pages[0], true
}

// handlePublicStatus serves a status page with no sign-in. A page that doesn't exist and one that's turned off look the same.
func (p *Panel) handlePublicStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	slug := strings.TrimPrefix(r.URL.Path, "/public/status/")
	var s StatusPage
	var ok bool
	if slug == "" {
		s, ok = onlyStatusPage()
	} else {
		s, ok = statusPageBySlug(slug)
	}
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if !ok || !s.Enabled {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8"><meta name="robots" content="noindex"><title>Not found</title><p style="font:16px system-ui;margin:4rem auto;max-width:30rem;padding:0 1rem">There's no status page here.</p>`))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=30")
	_, _ = w.Write(publicStatusCache.render(s, time.Now()))
}

var statusTmpl = template.Must(template.New("status").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="refresh" content="60">
<title>{{.Title}}</title>
<style>
:root { --bg:#f6f8f5; --panel:#fff; --line:#d5dfda; --fg:#12302c; --muted:#4b655f; --good:#0f6b4a; --warn:#7f6200; --bad:#b3261e; --none:#dde6e0; --seam:#a4531f; color-scheme:light }
@media (prefers-color-scheme: dark) { :root { --bg:#142c29; --panel:#19332f; --line:#2d4a45; --fg:#f0f4f1; --muted:#a6bcb6; --good:#8fdcb9; --warn:#f0d878; --bad:#f4978e; --none:#2d4a45; --seam:#e39155; color-scheme:dark } }
* { box-sizing:border-box }
body { margin:0; background:var(--bg); color:var(--fg); font:16px/1.5 ui-sans-serif,system-ui,-apple-system,"Segoe UI",sans-serif; -webkit-font-smoothing:antialiased }
.wrap { max-width:680px; margin:0 auto; padding:56px 20px 40px }
h1 { font-size:1rem; font-weight:600; margin:0 0 28px }
.head { font-size:clamp(1.6rem,5vw,2.25rem); font-weight:700; letter-spacing:-.02em; line-height:1.1; margin:0; padding-bottom:22px; border-bottom:2px solid var(--seam) }
.op .head { color:var(--good) } .major .head { color:var(--bad) } .partial .head { color:var(--warn) }
.row { padding:18px 0; border-bottom:1px solid var(--line) }
.top { display:flex; justify-content:space-between; gap:12px; flex-wrap:wrap }
.name { font-weight:600; overflow-wrap:anywhere } .state { font-size:.875rem; font-weight:600 }
.state.op { color:var(--good) } .state.down { color:var(--bad) } .state.idle { color:var(--muted) }
.bars { display:flex; gap:2px; margin-top:10px; height:26px }
.bars i { flex:1; min-width:0; border-radius:2px; background:var(--none) }
.bars .good { background:var(--good) } .bars .warn { background:var(--warn) } .bars .bad { background:var(--bad) }
.sub { display:flex; justify-content:space-between; gap:12px; margin-top:6px; color:var(--muted); font-size:.75rem }
h2 { font-size:.95rem; margin:36px 0 8px }
.ev { display:flex; justify-content:space-between; gap:12px; flex-wrap:wrap; padding:10px 0; border-bottom:1px solid var(--line); font-size:.875rem }
.ev span { color:var(--muted) }
.empty { color:var(--muted); font-size:.875rem; margin:8px 0 0 }
footer { margin-top:36px; color:var(--muted); font-size:.75rem; display:flex; justify-content:space-between; gap:12px; flex-wrap:wrap }
footer a { color:inherit }
</style>
</head>
<body class="{{.Class}}">
<main class="wrap">
<h1>{{.Title}}</h1>
<p class="head">{{.Headline}}</p>
{{range .Rows}}<section class="row">
<div class="top"><span class="name">{{.Name}}</span><span class="state {{.Class}}">{{.Label}}</span></div>
<div class="bars" role="img" aria-label="Uptime for the last 30 days">{{range .Bars}}<i class="{{.Class}}" title="{{.Label}}"></i>{{end}}</div>
<div class="sub"><span>30 days ago</span><span>{{.Pct}}</span><span>Today</span></div>
</section>{{end}}
<h2>Recent incidents</h2>
{{if .Events}}{{range .Events}}<div class="ev"><strong>{{.App}}</strong><span>{{.When}} · {{.Duration}}</span></div>{{end}}{{else}}<p class="empty">No incidents in the last 30 days.</p>{{end}}
<footer><span>Updated {{.Updated}}</span><a href="https://useskiff.xyz">Powered by Skiff</a></footer>
</main>
</body>
</html>
`))
