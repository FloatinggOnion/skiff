// Package router is Skiff's edge router: it discovers apps from Docker labels and reverse-proxies <app>.<domain> to them, with automatic HTTPS.
package router

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"github.com/nuelScript/skiff/internal/docker"
	"github.com/nuelScript/skiff/internal/mail"
	"github.com/nuelScript/skiff/internal/sleep"
)

type Router struct {
	Domains []string
	Engine  *docker.Engine
	Panel   string // fallback host:port of the control panel; dash.<domain> proxies here
	// PanelPointer, when set, is a file the panel rewrites during a zero-downtime self-deploy to flip dash.<domain> onto the new process without restarting the router.
	PanelPointer string
	SiteApp      string // app that serves the apex + www.<domain>
	// DomainsFile, when set, is a JSON file of custom host→app mappings read live (cached briefly) so domains change without restarting the edge.
	DomainsFile string
	// MetricsFile, when set, is where the router snapshots per-app request metrics for the panel's Analytics page.
	MetricsFile string
	// RequestsDir, when set, receives a daily JSONL log of each request to an app, which the panel reads for per-project insights.
	RequestsDir string
	// SleepFile lists apps the panel stopped for being idle; a visit to one starts it again.
	SleepFile string
	// MailFile is the JSON forwarding table (domain → alias → destinations) read live by the SMTP listener on SMTPAddr.
	MailFile string
	SMTPAddr string
	// GateFile lists previews that ask visitors for a password first.
	GateFile string

	metrics       *Metrics
	reqlog        *ReqLog
	mu            sync.RWMutex
	cachedPanel   string
	cachedAt      time.Time
	cachedDomains map[string]string
	cachedDomAt   time.Time
	rr            map[string]uint64 // per-app round-robin cursor across replicas
	cachedSleep   sleep.Apps
	cachedSleepAt time.Time
	wakeMu        sync.Mutex

	// mailMu is separate from mu so reading the forwarding table can't serialize against request routing.
	mailMu        sync.Mutex
	cachedRules   mail.Rules
	cachedRulesAt time.Time

	// routesMu is separate from mu so a Docker fetch never blocks lookups: requests are served from the last table while one refresh runs in the background.
	routesMu   sync.Mutex
	routeTable map[string][]int
	routeAt    time.Time
	refreshing bool

	rrMu sync.Mutex

	gate gateState
}

func (rt *Router) startMetrics() {
	if rt.MetricsFile != "" && rt.metrics == nil {
		rt.metrics = NewMetrics(rt.MetricsFile)
		go rt.metrics.Run()
	}
	if rt.RequestsDir != "" && rt.reqlog == nil {
		if rt.reqlog = NewReqLog(rt.RequestsDir); rt.reqlog != nil {
			go rt.reqlog.Run()
		}
	}
}

func (rt *Router) customDomains() map[string]string {
	if rt.DomainsFile == "" {
		return nil
	}
	rt.mu.RLock()
	m, at := rt.cachedDomains, rt.cachedDomAt
	rt.mu.RUnlock()
	if m != nil && time.Since(at) < 2*time.Second {
		return m
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.cachedDomains != nil && time.Since(rt.cachedDomAt) < 2*time.Second {
		return rt.cachedDomains
	}
	m = map[string]string{}
	if b, err := os.ReadFile(rt.DomainsFile); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	rt.cachedDomains, rt.cachedDomAt = m, time.Now()
	return m
}

func hostOnly(host string) string {
	if i := strings.IndexByte(host, ':'); i >= 0 {
		return host[:i]
	}
	return host
}

func (rt *Router) panelUpstream() string {
	if rt.PanelPointer == "" {
		return rt.Panel
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.cachedPanel != "" && time.Since(rt.cachedAt) < 2*time.Second {
		return rt.cachedPanel
	}
	up := rt.Panel
	if b, err := os.ReadFile(rt.PanelPointer); err == nil {
		if v := strings.TrimSpace(string(b)); v != "" {
			up = v
		}
	}
	rt.cachedPanel, rt.cachedAt = up, time.Now()
	return up
}

// StatusDomainPrefix marks a custom domain as a team's status page rather than an app.
const StatusDomainPrefix = "@status:"

// proxyStatus hands a status page to the panel, which serves them publicly; only that path, and only for reading.
func (rt *Router) proxyStatus(w http.ResponseWriter, r *http.Request, slug string) {
	up := rt.panelUpstream()
	if up == "" || (r.Method != http.MethodGet && r.Method != http.MethodHead) || !validStatusSlug(slug) {
		http.NotFound(w, r)
		return
	}
	out := r.Clone(r.Context())
	out.URL.Path, out.URL.RawPath, out.URL.RawQuery = "/public/status/"+slug, "", ""
	proxyTo(w, out, up)
}

func validStatusSlug(s string) bool {
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

func (rt *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	start := time.Now()
	label := rt.serve(rec, r)
	dur := time.Since(start)
	if rt.metrics != nil {
		rt.metrics.Record(label, rec.status, r.ContentLength, rec.bytes, dur)
	}
	if label != "" && label != "dash" && label != "status" {
		rt.reqlog.Record(label, r, rec.status, rec.bytes, dur)
	}
}

// serve routes a request and returns the app label it's accounted to (empty when nothing matched, so it isn't recorded).
func (rt *Router) serve(w http.ResponseWriter, r *http.Request) string {
	// A custom domain pointed at one of the apps wins over subdomain routing.
	if app, ok := rt.customDomains()[hostOnly(r.Host)]; ok {
		if slug, isStatus := strings.CutPrefix(app, StatusDomainPrefix); isStatus {
			rt.proxyStatus(w, r, slug)
			return "status"
		}
		if !rt.proxyToApp(w, r, app) {
			return ""
		}
		return app
	}

	sub, ok := rt.subFor(r.Host)
	if !ok {
		http.Error(w, "skiff: no app for "+r.Host, http.StatusNotFound)
		return ""
	}

	switch sub {
	case "status":
		rt.proxyStatus(w, r, strings.Trim(r.URL.Path, "/"))
		return "status"
	case "dash":
		if up := rt.panelUpstream(); up != "" {
			proxyTo(w, r, up)
			return "dash"
		}
	}

	app := sub
	if sub == "" || sub == "www" {
		app = rt.SiteApp
	}
	if app == "" {
		http.Error(w, "skiff: no app for "+r.Host, http.StatusNotFound)
		return ""
	}
	// Scanners probe random subdomains; only a real app gets requests counted against it.
	if !rt.proxyToApp(w, r, app) {
		return ""
	}
	return app
}

const (
	routesFresh = 2 * time.Second
	// A table older than this means the edge has been idle; it could point at retired containers, so wait for a new one rather than serve it.
	routesTooOld = 15 * time.Second
)

// routes returns the app→ports table. A stale table is served while one background refresh replaces it, so no request ever waits on Docker, however many containers there are. If Docker can't answer, the last table keeps serving.
func (rt *Router) routes() (map[string][]int, error) {
	rt.routesMu.Lock()
	defer rt.routesMu.Unlock()
	age := time.Since(rt.routeAt)
	if rt.routeTable != nil && age < routesFresh {
		return rt.routeTable, nil
	}
	if rt.routeTable != nil && age < routesTooOld {
		if !rt.refreshing {
			rt.refreshing = true
			go rt.refreshRoutes()
		}
		return rt.routeTable, nil
	}
	table, err := rt.fetchRoutes()
	if err != nil {
		if rt.routeTable != nil {
			return rt.routeTable, nil
		}
		return nil, err
	}
	rt.routeTable, rt.routeAt = table, time.Now()
	return table, nil
}

func (rt *Router) fetchRoutes() (map[string][]int, error) {
	routes, err := rt.Engine.Routes()
	if err != nil {
		return nil, err
	}
	table := make(map[string][]int, len(routes))
	for _, r := range routes {
		table[r.App] = append(table[r.App], r.HostPort)
	}
	return table, nil
}

func (rt *Router) refreshRoutes() {
	table, err := rt.fetchRoutes()
	rt.routesMu.Lock()
	defer rt.routesMu.Unlock()
	rt.refreshing = false
	if err != nil {
		log.Printf("router: reading routes from docker: %v", err)
		return
	}
	rt.routeTable, rt.routeAt = table, time.Now()
}

func (rt *Router) proxyToApp(w http.ResponseWriter, r *http.Request, app string) bool {
	if !rt.admit(w, r, app) {
		return true
	}
	if _, err := rt.routes(); err != nil {
		http.Error(w, "skiff: "+err.Error(), http.StatusInternalServerError)
		return true
	}
	ports := rt.portsFor(app)
	if len(ports) == 0 {
		ports = rt.wake(app)
	}
	if len(ports) == 0 {
		http.Error(w, "skiff: no app named "+app, http.StatusNotFound)
		return false
	}
	rt.rrMu.Lock()
	if rt.rr == nil {
		rt.rr = map[string]uint64{}
	}
	port := ports[rt.rr[app]%uint64(len(ports))]
	rt.rr[app]++
	rt.rrMu.Unlock()
	proxyTo(w, r, fmt.Sprintf("127.0.0.1:%d", port))
	return true
}

// upstream keeps connections to apps open between requests. The default keeps two per app, so under load almost every request opens a new one and the box runs out of local ports.
var upstream = &http.Transport{
	DialContext:         (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	MaxIdleConns:        8192,
	MaxIdleConnsPerHost: 2048,
	IdleConnTimeout:     60 * time.Second,
	DisableCompression:  true,
}

type proxyBuffers struct{ sync.Pool }

func (b *proxyBuffers) Get() []byte  { return *(b.Pool.Get().(*[]byte)) }
func (b *proxyBuffers) Put(p []byte) { b.Pool.Put(&p) }

var proxyBufs = &proxyBuffers{Pool: sync.Pool{New: func() any {
	b := make([]byte, 32*1024)
	return &b
}}}

func proxyTo(w http.ResponseWriter, r *http.Request, hostport string) {
	target := &url.URL{Scheme: "http", Host: hostport}
	rp := &httputil.ReverseProxy{
		Transport:  upstream,
		BufferPool: proxyBufs,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.SetXForwarded()
			pr.Out.Host = pr.In.Host
		},
	}
	rp.ServeHTTP(w, r)
}

// subFor returns a served host's leading label ("" for the apex); ok is false when the host is under no served domain.
func (rt *Router) subFor(host string) (string, bool) {
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	for _, d := range rt.Domains {
		if host == d {
			return "", true
		}
		if s := "." + d; strings.HasSuffix(host, s) {
			return strings.TrimSuffix(host, s), true
		}
	}
	return "", false
}

// certAllowed limits certificates to names Skiff actually serves. Wildcard DNS sends every name here, and scanners trying random subdomains would otherwise use up the certificate authority's weekly limit for the whole domain.
func (rt *Router) certAllowed(host string) bool {
	if _, ok := rt.customDomains()[host]; ok {
		return true
	}
	if host == rt.MailHost() {
		return true
	}
	sub, ok := rt.subFor(host)
	if !ok {
		return false
	}
	switch sub {
	case "", "www", "dash", "status":
		return true
	}
	if strings.Contains(sub, ".") {
		return false
	}
	if len(rt.portsFor(sub)) > 0 {
		return true
	}
	if _, ok := rt.sleepingApps()[sub]; ok {
		return true
	}
	_, ok = rt.protectedApps()[sub]
	return ok
}

// edgeServer sets ReadHeaderTimeout (closes Slowloris slow-header attacks) and IdleTimeout, but leaves Read/Write timeouts unset so proxied SSE/WebSocket streams aren't cut off mid-stream.
func edgeServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}

func (rt *Router) ServeHTTPOnly(addr string) error {
	rt.startMetrics()
	rt.startMail(nil)
	return edgeServer(addr, rt).ListenAndServe()
}

func (rt *Router) ServeTLS(cacheDir string) error {
	rt.startMetrics()
	m := &autocert.Manager{
		Prompt: autocert.AcceptTOS,
		Cache:  autocert.DirCache(cacheDir),
		HostPolicy: func(_ context.Context, host string) error {
			if rt.certAllowed(host) {
				return nil
			}
			return fmt.Errorf("host not allowed: %s", host)
		},
	}
	rt.startMail(m.GetCertificate)
	redirect := edgeServer(":80", m.HTTPHandler(nil))
	go func() {
		if err := redirect.ListenAndServe(); err != nil {
			log.Printf("router :80 listener stopped: %v", err)
		}
	}()
	server := edgeServer(":443", rt)
	server.TLSConfig = m.TLSConfig()
	return server.ListenAndServeTLS("", "")
}
