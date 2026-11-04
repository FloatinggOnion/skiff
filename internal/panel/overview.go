package panel

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/nuelScript/skiff/internal/registry"
)

const (
	diskWarnPct = 85
	memWarnPct  = 90
)

// attentionItem is something on the home screen a person should act on.
type attentionItem struct {
	Kind    string `json:"kind"` // deploy.failed | app.down | oom | disk | memory
	App     string `json:"app,omitempty"`
	State   string `json:"state,omitempty"`
	At      int64  `json:"at,omitempty"`
	Serving bool   `json:"serving,omitempty"` // an older version is still taking traffic
	Percent int    `json:"percent,omitempty"`
}

type serverUsage struct {
	Mem  resourceUsage `json:"mem"`
	Disk resourceUsage `json:"disk"`
}

const (
	oomShownFor  = time.Hour
	oomAlertGap  = 15 * time.Minute
	oomRetryWait = 5 * time.Second
)

// oomLog remembers when each app last ran out of memory. A killed container restarts straight away, so without this it would look like nothing happened.
var oomLog = struct {
	sync.Mutex
	last map[string]time.Time
}{last: map[string]time.Time{}}

// noteOOM records the kill and reports whether it should alert, which it does at most once per gap so a crash loop can't flood anyone.
func noteOOM(app string, now time.Time) bool {
	oomLog.Lock()
	defer oomLog.Unlock()
	prev, seen := oomLog.last[app]
	oomLog.last[app] = now
	return !seen || now.Sub(prev) >= oomAlertGap
}

func recentOOM(app string, now time.Time) (time.Time, bool) {
	oomLog.Lock()
	defer oomLog.Unlock()
	t, ok := oomLog.last[app]
	return t, ok && now.Sub(t) < oomShownFor
}

func (p *Panel) oomLoop() {
	for {
		err := p.eng.WatchOOM(context.Background(), func(app, container string) {
			log.Printf("skiff: %s ran out of memory (%s)", app, container)
			src, ok := getSource(app)
			if ok && noteOOM(app, time.Now()) {
				go dispatchAlert(alertEvent{
					Team: src.Team, Kind: "app.oom", App: app,
					Title:  app + " ran out of memory",
					Detail: "It hit its memory limit and was restarted. Raise its limit in project settings, or look for a leak.",
				})
			}
		})
		if err != nil {
			time.Sleep(oomRetryWait)
		}
	}
}

func usageOf() serverUsage { return serverUsage{Mem: procMem(), Disk: diskUsage()} }

func pct(u resourceUsage) int {
	if u.Total <= 0 {
		return 0
	}
	return int(u.Used * 100 / u.Total)
}

// projectAttention decides what, if anything, a project needs: a failed last deploy says so even when an older version is still up, and never having run at all counts too.
func projectAttention(app, state string, deployed bool, last *Deploy) (attentionItem, bool) {
	up := deployed && (state == "running" || state == "sleeping")
	if last != nil && last.Status == "building" {
		return attentionItem{}, false
	}
	if last != nil && last.Status == "failed" {
		return attentionItem{Kind: "deploy.failed", App: app, State: state, At: last.Started, Serving: up}, true
	}
	if deployed && !up {
		return attentionItem{Kind: "app.down", App: app, State: state}, true
	}
	return attentionItem{}, false
}

func usageAttention(u serverUsage) []attentionItem {
	var out []attentionItem
	if d := pct(u.Disk); d >= diskWarnPct {
		out = append(out, attentionItem{Kind: "disk", Percent: d})
	}
	if m := pct(u.Mem); m >= memWarnPct {
		out = append(out, attentionItem{Kind: "memory", Percent: m})
	}
	return out
}

func (p *Panel) teamProjects(team string) []Source {
	rows, err := sqlDB.Query(`SELECT `+sourceCols+` FROM sources WHERE team=? AND parent='' ORDER BY app`, team)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Source
	for rows.Next() {
		if s, ok := scanSource(rows); ok {
			out = append(out, s)
		}
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

// handleOverview answers "is everything OK?" for the team's home screen. The server's own numbers go only to owners, as on the Server page.
func (p *Panel) handleOverview(w http.ResponseWriter, r *http.Request) {
	team := p.teamID(r)
	if team == "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	apps, err := registry.Load()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	attention := []attentionItem{}
	building := []string{}
	running, sleeping := 0, 0
	projects := p.teamProjects(team)
	for _, src := range projects {
		a, deployed := apps[src.App]
		state := "missing"
		if deployed {
			state = p.displayState(src.App, a.Container)
		}
		switch state {
		case "running":
			running++
		case "sleeping":
			sleeping++
		}
		var last *Deploy
		if ds := appDeploysPage(src.App, 0, "", 1); len(ds) > 0 {
			last = &ds[0]
		}
		if last != nil && last.Status == "building" {
			building = append(building, src.App)
		}
		if item, ok := projectAttention(src.App, state, deployed, last); ok {
			attention = append(attention, item)
		} else if t, oom := recentOOM(src.App, time.Now()); oom {
			attention = append(attention, attentionItem{Kind: "oom", App: src.App, At: t.Unix()})
		}
	}
	if p.isInstanceAdmin(r) {
		attention = append(attention, usageAttention(usageOf())...)
	}
	writeJSON(w, map[string]any{
		"projects": len(projects), "running": running, "sleeping": sleeping,
		"building": building, "attention": attention,
	})
}

// handleServerUsage feeds the sidebar's gauge; light enough to poll from every page.
func (p *Panel) handleServerUsage(w http.ResponseWriter, r *http.Request) {
	if !p.isInstanceAdmin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	writeJSON(w, usageOf())
}
