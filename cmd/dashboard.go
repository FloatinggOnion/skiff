package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nuelScript/skiff/internal/docker"
	"github.com/nuelScript/skiff/internal/proxy"
	"github.com/nuelScript/skiff/internal/registry"
	"github.com/nuelScript/skiff/internal/ui"
	"github.com/spf13/cobra"
)

func newDashboardCmd() *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Serve a local web dashboard of your apps",
		RunE: func(cmd *cobra.Command, args []string) error {
			mux := http.NewServeMux()
			mux.HandleFunc("/api/apps", handleAppsAPI)
			mux.HandleFunc("/api/logs", handleLogsStream)
			mux.HandleFunc("/api/down", handleDown)
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/" {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				fmt.Fprint(w, dashboardHTML)
			})

			ui.Banner(version)
			ui.Field("dashboard", "http://localhost"+addr)
			ui.Note("live view of your apps  (ctrl-c to stop)")
			fmt.Println()
			server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second}
			return server.ListenAndServe()
		},
	}
	cmd.Flags().StringVar(&addr, "addr", ":4000", "address to listen on")
	return cmd
}

type appStatus struct {
	Name     string `json:"name"`
	Target   string `json:"target"`
	State    string `json:"state"`
	Health   string `json:"health"`
	URL      string `json:"url"`
	HostPort int    `json:"hostPort"`
}

func handleAppsAPI(w http.ResponseWriter, r *http.Request) {
	apps, err := registry.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	out := make([]appStatus, 0, len(apps))
	for _, a := range apps {
		state := docker.For(a.Host).State(a.Container)

		url := proxy.URL(a.Name)
		target := "local"
		if a.Host != "" {
			url = ""
			target = a.Host
		}

		health := "—"
		if state == "running" {
			if probeApp(a) {
				health = "healthy"
			} else {
				health = "unreachable"
			}
		}

		out = append(out, appStatus{
			Name:     a.Name,
			Target:   target,
			State:    state,
			Health:   health,
			URL:      url,
			HostPort: a.HostPort,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func handleLogsStream(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("app")
	apps, err := registry.Load()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	app, ok := apps[name]
	if !ok {
		http.Error(w, "unknown app", http.StatusNotFound)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	pr, pw := io.Pipe()
	go func() {
		_ = docker.For(app.Host).StreamLogs(r.Context(), app.Container, pw)
		pw.Close()
	}()

	sc := bufio.NewScanner(pr)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		fmt.Fprintf(w, "data: %s\n\n", sc.Text())
		flusher.Flush()
	}
}

func handleDown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := r.URL.Query().Get("app")
	apps, err := registry.Load()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	app, ok := apps[name]
	if !ok {
		http.Error(w, "unknown app", http.StatusNotFound)
		return
	}
	_ = docker.For(app.Host).Remove(app.Container)
	if app.Host != "" {
		_ = registry.DeleteRemote(app.Host, name)
	}
	_, _ = registry.Delete(name)
	w.WriteHeader(http.StatusNoContent)
}

const dashboardHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Skiff</title>
<style>
  :root { --bg:#0b0b0f; --card:#15151c; --line:#26262f; --text:#e5e5ea; --muted:#8a8a99; --accent:#7C3AED; --green:#16A34A; --red:#DC2626; }
  * { box-sizing:border-box; }
  body { margin:0; background:var(--bg); color:var(--text); font:14px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace; }
  header { display:flex; align-items:baseline; gap:12px; padding:24px 28px; border-bottom:1px solid var(--line); }
  header h1 { margin:0; font-size:18px; color:var(--accent); letter-spacing:.5px; }
  header .count { color:var(--muted); font-size:13px; }
  main { padding:20px 28px 50vh; display:grid; gap:12px; grid-template-columns:repeat(auto-fill,minmax(320px,1fr)); }
  .card { background:var(--card); border:1px solid var(--line); border-radius:10px; padding:16px 18px; }
  .top { display:flex; justify-content:space-between; align-items:center; margin-bottom:12px; }
  .name { font-size:15px; font-weight:600; }
  .badge { font-size:11px; padding:2px 9px; border-radius:999px; border:1px solid var(--line); color:var(--muted); text-transform:uppercase; letter-spacing:.5px; }
  .badge.running { color:var(--green); border-color:var(--green); }
  .badge.exited, .badge.missing { color:var(--red); border-color:var(--red); }
  .row { display:flex; justify-content:space-between; color:var(--muted); font-size:12.5px; padding:2px 0; }
  .row b { color:var(--text); font-weight:500; }
  a { color:var(--accent); text-decoration:none; }
  a:hover { text-decoration:underline; }
  .logs-link { color:var(--muted); }
  .logs-link:hover { color:var(--accent); }
  .stop-link { color:var(--muted); }
  .stop-link:hover { color:var(--red); }
  .empty { color:var(--muted); padding:48px; text-align:center; grid-column:1/-1; }
  #panel { position:fixed; left:0; right:0; bottom:0; height:45vh; background:#0d0d13; border-top:1px solid var(--line); display:none; flex-direction:column; }
  #panel.open { display:flex; }
  .phead { display:flex; justify-content:space-between; align-items:center; padding:10px 16px; border-bottom:1px solid var(--line); }
  .ptitle { color:var(--accent); font-size:13px; }
  .pclose { cursor:pointer; color:var(--muted); background:none; border:none; font:inherit; }
  .pclose:hover { color:var(--text); }
  #plog { margin:0; padding:12px 16px; overflow:auto; flex:1; font-size:12px; color:#c9c9d4; white-space:pre-wrap; }
</style>
</head>
<body>
<header><h1>Skiff</h1><span class="count" id="count"></span></header>
<main id="apps"></main>
<div id="panel"><div class="phead"><span class="ptitle" id="ptitle"></span><button class="pclose" onclick="closeLogs()">close</button></div><pre id="plog"></pre></div>
<script>
function esc(s){ return String(s).replace(/[&<>"]/g, function(c){ return {'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]; }); }
async function load(){
  try {
    var apps = await (await fetch('/api/apps')).json();
    document.getElementById('count').textContent = apps.length ? apps.length + (apps.length>1?' apps':' app') : '';
    var main = document.getElementById('apps');
    if(!apps.length){ main.innerHTML = '<div class="empty">No apps deployed yet &mdash; run <b>skiff deploy</b>.</div>'; return; }
    main.innerHTML = apps.map(function(a){
      var host = esc(a.url.replace(/^https?:\/\//,''));
      return '<div class="card"><div class="top"><span class="name">'+esc(a.name)+'</span>'
        + '<span class="badge '+esc(a.state)+'">'+esc(a.state)+'</span></div>'
        + '<div class="row"><span>health</span><b>'+esc(a.health)+'</b></div>'
        + '<div class="row"><span>target</span><b>'+esc(a.target)+'</b></div>'
        + '<div class="row"><span>url</span><a href="'+esc(a.url)+'" target="_blank">'+host+'</a></div>'
        + '<div class="row"><span>logs</span><span><a href="#" class="logs-link" data-app="'+esc(a.name)+'">view</a> &middot; <a href="#" class="stop-link" data-app="'+esc(a.name)+'">stop</a></span></div>'
        + '</div>';
    }).join('');
  } catch(e){}
}
var es = null;
function openLogs(name){
  closeLogs();
  document.getElementById('ptitle').textContent = name + ' — logs';
  var log = document.getElementById('plog'); log.textContent = '';
  document.getElementById('panel').classList.add('open');
  es = new EventSource('/api/logs?app=' + encodeURIComponent(name));
  es.onmessage = function(ev){ log.textContent += ev.data + '\n'; log.scrollTop = log.scrollHeight; };
}
function closeLogs(){
  if(es){ es.close(); es = null; }
  document.getElementById('panel').classList.remove('open');
}
document.addEventListener('click', function(e){
  var t = e.target;
  if(!t || !t.classList) return;
  if(t.classList.contains('logs-link')){ e.preventDefault(); openLogs(t.getAttribute('data-app')); }
  if(t.classList.contains('stop-link')){ e.preventDefault(); var n = t.getAttribute('data-app'); if(confirm('Stop '+n+'?')){ fetch('/api/down?app='+encodeURIComponent(n), {method:'POST'}).then(load); } }
});
load(); setInterval(load, 3000);
</script>
</body>
</html>`
