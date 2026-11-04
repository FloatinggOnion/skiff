package router

import (
	"html/template"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nuelScript/skiff/internal/gate"
)

const (
	unlockPath   = "/__skiff/unlock"
	gateLifetime = 7 * 24 * time.Hour
	maxFailures  = 10
	failWindow   = 10 * time.Minute
	// Wrong guesses allowed per app across all visitors, and the most keys tracked at once.
	maxAppFailures = 2000
	maxGateKeys    = 10_000
	maxGateWork    = 4
)

// gateWork bounds password hashing running at once, so guesses can't use up the edge's CPU.
var gateWork = make(chan struct{}, maxGateWork)

type gateState struct {
	mu       sync.Mutex
	apps     gate.Apps
	modTime  time.Time
	size     int64
	failures map[string][]time.Time // app|visitor and app → recent wrong passwords
}

// protectedApps re-reads the list as soon as it changes rather than on a timer, so a preview that comes up within seconds of being locked is never served open.
func (rt *Router) protectedApps() gate.Apps {
	if rt.GateFile == "" {
		return nil
	}
	fi, err := os.Stat(rt.GateFile)
	rt.gate.mu.Lock()
	defer rt.gate.mu.Unlock()
	if err != nil {
		rt.gate.apps = nil
		return nil
	}
	if rt.gate.apps == nil || !fi.ModTime().Equal(rt.gate.modTime) || fi.Size() != rt.gate.size {
		rt.gate.apps, rt.gate.modTime, rt.gate.size = gate.Load(rt.GateFile), fi.ModTime(), fi.Size()
	}
	return rt.gate.apps
}

// admit reports whether the request may reach the app; otherwise it has already answered with the password page.
func (rt *Router) admit(w http.ResponseWriter, r *http.Request, app string) bool {
	hash, ok := rt.protectedApps()[app]
	if !ok {
		return true
	}
	if r.URL.Path == unlockPath && r.Method == http.MethodPost {
		rt.unlock(w, r, app, hash)
		return false
	}
	if c, err := r.Cookie(gate.CookieName); err == nil && gate.ValidCookie(hash, app, c.Value, time.Now()) {
		return true
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "this preview is password protected", http.StatusUnauthorized)
		return false
	}
	askPassword(w, r.URL.RequestURI(), "", http.StatusUnauthorized)
	return false
}

func (rt *Router) unlock(w http.ResponseWriter, r *http.Request, app, hash string) {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	_ = r.ParseForm()
	next := safeNext(r.PostForm.Get("next"))
	visitor := app + "|" + limitKey(r)
	if !rt.allowAttempt(visitor, app) {
		askPassword(w, next, "Too many attempts. Try again in a few minutes.", http.StatusTooManyRequests)
		return
	}
	select {
	case gateWork <- struct{}{}:
	case <-time.After(2 * time.Second):
		w.Header().Set("Retry-After", "5")
		askPassword(w, next, "Busy right now. Try again in a moment.", http.StatusServiceUnavailable)
		return
	}
	ok := gate.Check(hash, r.PostForm.Get("password"))
	<-gateWork
	if !ok {
		rt.noteFailure(visitor, app)
		askPassword(w, next, "That password isn't right.", http.StatusUnauthorized)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: gate.CookieName, Value: gate.Cookie(hash, app, time.Now().Add(gateLifetime)),
		Path: "/", MaxAge: int(gateLifetime.Seconds()), HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// recent returns the unexpired failures for key, dropping the key once none remain.
func (g *gateState) recent(key string) []time.Time {
	recent := g.failures[key][:0]
	for _, t := range g.failures[key] {
		if time.Since(t) < failWindow {
			recent = append(recent, t)
		}
	}
	if len(recent) == 0 {
		delete(g.failures, key)
	} else {
		g.failures[key] = recent
	}
	return recent
}

// allowAttempt checks the visitor's own failures and the app's total, so spreading guesses across addresses doesn't help.
func (rt *Router) allowAttempt(visitor, app string) bool {
	rt.gate.mu.Lock()
	defer rt.gate.mu.Unlock()
	return len(rt.gate.recent(visitor)) < maxFailures && len(rt.gate.recent(app)) < maxAppFailures
}

func (rt *Router) noteFailure(visitor, app string) {
	rt.gate.mu.Lock()
	defer rt.gate.mu.Unlock()
	if rt.gate.failures == nil {
		rt.gate.failures = map[string][]time.Time{}
	}
	if len(rt.gate.failures) >= maxGateKeys {
		rt.gate.evictOldest()
	}
	now := time.Now()
	rt.gate.failures[visitor] = append(rt.gate.failures[visitor], now)
	rt.gate.failures[app] = append(rt.gate.failures[app], now)
}

// evictOldest drops the tenth of keys whose latest failure is oldest, so a flood of addresses forgets the stalest history rather than everyone's.
func (g *gateState) evictOldest() {
	type entry struct {
		key  string
		last time.Time
	}
	all := make([]entry, 0, len(g.failures))
	for k, ts := range g.failures {
		last := time.Time{}
		if len(ts) > 0 {
			last = ts[len(ts)-1]
		}
		all = append(all, entry{k, last})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].last.Before(all[j].last) })
	for _, e := range all[:len(all)/10+1] {
		delete(g.failures, e.key)
	}
}

// safeNext keeps the post-unlock redirect on this host; browsers drop tabs and newlines inside a URL, so "/\t/host" would otherwise leave it.
func safeNext(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, unlockPath) {
		return "/"
	}
	for _, c := range next {
		if c < 0x20 || c == 0x7f || c == '\\' {
			return "/"
		}
	}
	return next
}

var askTmpl = template.Must(template.New("ask").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="robots" content="noindex"><title>Password required</title>
<style>
:root{color-scheme:light dark;--bg:#fafafa;--fg:#111;--muted:#666;--line:#ddd;--err:#c62828}
@media (prefers-color-scheme:dark){:root{--bg:#0a0a0a;--fg:#ededed;--muted:#999;--line:#2a2a2a;--err:#ff6b6b}}
*{box-sizing:border-box}body{margin:0;min-height:100vh;display:grid;place-items:center;background:var(--bg);color:var(--fg);font:15px/1.5 system-ui,-apple-system,sans-serif;padding:16px}
form{width:100%;max-width:340px}h1{font-size:18px;margin:0 0 4px}p{margin:0 0 20px;color:var(--muted);font-size:14px}
input{width:100%;padding:10px 12px;border:1px solid var(--line);border-radius:8px;background:transparent;color:inherit;font:inherit}
button{width:100%;margin-top:10px;padding:10px;border:0;border-radius:8px;background:var(--fg);color:var(--bg);font:inherit;font-weight:600;cursor:pointer}
.err{color:var(--err);font-size:13px;margin:8px 0 0}
</style></head><body>
<form method="post" action="/__skiff/unlock">
<h1>Password required</h1><p>This preview is protected. Enter the password to view it.</p>
<input type="hidden" name="next" value="{{.Next}}">
<input type="password" name="password" autocomplete="current-password" aria-label="Password" placeholder="Password" required autofocus>
{{if .Error}}<p class="err" role="alert">{{.Error}}</p>{{end}}
<button type="submit">Continue</button>
</form></body></html>`))

func askPassword(w http.ResponseWriter, next, msg string, status int) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Robots-Tag", "noindex")
	h.Set("X-Frame-Options", "DENY")
	w.WriteHeader(status)
	_ = askTmpl.Execute(w, struct{ Next, Error string }{safeNext(next), msg})
}
