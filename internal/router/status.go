package router

import (
	"html/template"
	"net/http"
	"sort"
)

type serviceView struct {
	Name  string
	Label string
	Class string // op | down | idle
}

type statusView struct {
	Class    string // op | partial | major | empty
	Headline string
	Services []serviceView
}

func (rt *Router) serveStatus(w http.ResponseWriter, _ *http.Request) {
	states, _ := rt.Engine.AppStates()

	names := make([]string, 0, len(states))
	for n := range states {
		if n == rt.SiteApp {
			continue // the marketing site itself isn't a listed service
		}
		names = append(names, n)
	}
	sort.Strings(names)

	asleep := rt.sleepingApps()
	up := 0
	svcs := make([]serviceView, 0, len(names))
	for _, n := range names {
		v := serviceView{Name: n}
		state := states[n]
		if _, ok := asleep[n]; ok {
			state = "running" // stopped while idle; the next visit starts it
		}
		switch state {
		case "running":
			v.Label, v.Class = "Operational", "op"
			up++
		case "created", "paused", "restarting":
			v.Label, v.Class = "Degraded", "idle"
		default:
			v.Label, v.Class = "Down", "down"
		}
		svcs = append(svcs, v)
	}

	view := statusView{Services: svcs}
	switch {
	case len(svcs) == 0:
		view.Class, view.Headline = "empty", "No services deployed yet"
	case up == len(svcs):
		view.Class, view.Headline = "op", "All systems operational"
	case up == 0:
		view.Class, view.Headline = "major", "Major outage"
	default:
		view.Class, view.Headline = "partial", "Partial outage"
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = statusTmpl.Execute(w, view)
}

var statusTmpl = template.Must(template.New("status").Parse(statusHTML))

const statusHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="refresh" content="20">
<title>Skiff · Status</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Geist:wght@400;500;600&family=Geist+Mono&display=swap" rel="stylesheet">
<style>
  :root { --bg:#000; --surface:#0a0a0a; --line:#232326; --fg:#fafafa; --muted:#a1a1a1; --subtle:#6e6e6e;
          --op:#10b981; --down:#ef4444; --idle:#a1a1a1; --partial:#f59e0b; }
  * { box-sizing:border-box; }
  body { margin:0; background:var(--bg); color:var(--fg); font-family:'Geist',system-ui,sans-serif; -webkit-font-smoothing:antialiased; }
  .wrap { max-width:640px; margin:0 auto; padding:64px 24px; }
  header { display:flex; align-items:center; gap:8px; margin-bottom:40px; }
  header .name { font-weight:600; letter-spacing:-.01em; }
  header .tag { color:var(--subtle); font-family:'Geist Mono',monospace; font-size:12px; }
  .banner { display:flex; align-items:center; gap:12px; border:1px solid var(--line); background:var(--surface); border-radius:14px; padding:20px 22px; margin-bottom:24px; }
  .banner .dot { width:11px; height:11px; border-radius:50%; flex:none; }
  .banner h1 { margin:0; font-size:18px; font-weight:600; letter-spacing:-.01em; }
  .banner.op .dot { background:var(--op); box-shadow:0 0 12px color-mix(in oklab,var(--op) 65%,transparent); }
  .banner.partial .dot { background:var(--partial); }
  .banner.major .dot { background:var(--down); }
  .banner.empty .dot { background:var(--subtle); }
  .list { border:1px solid var(--line); border-radius:14px; overflow:hidden; }
  .row { display:flex; align-items:center; justify-content:space-between; padding:16px 20px; border-top:1px solid var(--line); }
  .row:first-child { border-top:0; }
  .row .svc { font-weight:500; }
  .row .st { display:flex; align-items:center; gap:8px; font-family:'Geist Mono',monospace; font-size:11.5px; color:var(--muted); text-transform:uppercase; letter-spacing:.05em; }
  .row .st .dot { width:8px; height:8px; border-radius:50%; }
  .st .dot.op { background:var(--op); box-shadow:0 0 8px color-mix(in oklab,var(--op) 60%,transparent); }
  .st .dot.down { background:var(--down); }
  .st .dot.idle { background:var(--idle); }
  .empty-row { padding:44px; text-align:center; color:var(--subtle); font-size:14px; }
  footer { margin-top:28px; display:flex; justify-content:space-between; color:var(--subtle); font-family:'Geist Mono',monospace; font-size:11px; }
</style>
</head>
<body>
  <div class="wrap">
    <header>
      <svg width="22" height="22" viewBox="0 0 32 32" fill="none">
        <path d="M17.5 3 Q25.5 16 27 24 H17.5 Z" fill="#fafafa"/>
        <path d="M14.3 8 Q9 17.5 6 24 H14.3 Z" fill="#fafafa" fill-opacity="0.55"/>
        <path d="M4 26.6 H28" stroke="#a1a1a1" stroke-width="1.8" stroke-linecap="round"/>
      </svg>
      <span class="name">Skiff</span><span class="tag">/ status</span>
    </header>

    <div class="banner {{.Class}}">
      <span class="dot"></span>
      <h1>{{.Headline}}</h1>
    </div>

    {{if .Services}}
    <div class="list">
      {{range .Services}}
      <div class="row">
        <span class="svc">{{.Name}}</span>
        <span class="st"><span class="dot {{.Class}}"></span>{{.Label}}</span>
      </div>
      {{end}}
    </div>
    {{else}}
    <div class="list"><div class="empty-row">No services deployed yet.</div></div>
    {{end}}

    <footer>
      <span>Powered by Skiff</span>
      <span>auto-refreshes every 20s</span>
    </footer>
  </div>
</body>
</html>`
