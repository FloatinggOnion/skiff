package router

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nuelScript/skiff/internal/docker"
)

func TestSubFor(t *testing.T) {
	rt := &Router{Domains: []string{"useskiff.xyz", "example.com"}}
	cases := []struct {
		host    string
		wantSub string
		wantOK  bool
	}{
		{"useskiff.xyz", "", true},                        // apex
		{"useskiff.xyz:443", "", true},                    // apex with port
		{"dash.useskiff.xyz", "dash", true},               // reserved subdomain
		{"blog.example.com", "blog", true},                // second served domain
		{"api.staging.useskiff.xyz", "api.staging", true}, // multi-label sub
		{"unknown.com", "", false},                        // not a served domain
		{"notuseskiff.xyz", "", false},                    // suffix match but not a subdomain
	}
	for _, c := range cases {
		t.Run(c.host, func(t *testing.T) {
			sub, ok := rt.subFor(c.host)
			if sub != c.wantSub || ok != c.wantOK {
				t.Fatalf("subFor(%q) = (%q,%v), want (%q,%v)", c.host, sub, ok, c.wantSub, c.wantOK)
			}
		})
	}
}

func TestHostOnly(t *testing.T) {
	cases := map[string]string{
		"dash.useskiff.xyz":     "dash.useskiff.xyz",
		"dash.useskiff.xyz:443": "dash.useskiff.xyz",
		"localhost:8080":        "localhost",
	}
	for in, want := range cases {
		if got := hostOnly(in); got != want {
			t.Errorf("hostOnly(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestServeOnlyCountsRealApps(t *testing.T) {
	rt := &Router{Domains: []string{"example.com"}, SiteApp: "www"}
	rt.routeTable = map[string][]int{"shop": {1}}
	rt.routeAt = time.Now().Add(time.Hour)

	r := httptest.NewRequest("GET", "https://www.www.x7k2.dash.example.com/", nil)
	w := httptest.NewRecorder()
	if label := rt.serve(w, r); label != "" || w.Code != http.StatusNotFound {
		t.Fatalf("unknown subdomain counted as %q (status %d)", label, w.Code)
	}
}

func TestAnswersNeedsAnHTTPReply(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	// Like docker's port proxy before the app is up: accept, then hang up without a reply.
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	if answers(port) {
		t.Fatal("a bare TCP accept counted as ready")
	}
	ln.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }))
	defer srv.Close()
	if !answers(srv.Listener.Addr().(*net.TCPAddr).Port) {
		t.Fatal("an app answering 404 should count as ready")
	}
}

func TestCertAllowedOnlyForServedNames(t *testing.T) {
	dir := t.TempDir()
	domains := filepath.Join(dir, "domains.json")
	_ = os.WriteFile(domains, []byte(`{"shop.example.org":"shop"}`), 0o644)
	sleeping := filepath.Join(dir, "sleeping.json")
	_ = os.WriteFile(sleeping, []byte(`{"napper":["c1"]}`), 0o644)
	rt := &Router{Domains: []string{"useskiff.xyz", "1-2-3-4.sslip.io"}, DomainsFile: domains, SleepFile: sleeping}
	rt.routeTable = map[string][]int{"shop": {1}}
	rt.routeAt = time.Now().Add(time.Hour)

	for host, want := range map[string]bool{
		"useskiff.xyz":                   true,
		"www.useskiff.xyz":               true,
		"dash.useskiff.xyz":              true,
		"status.1-2-3-4.sslip.io":        true,
		"mx.useskiff.xyz":                true,
		"shop.useskiff.xyz":              true,
		"shop.1-2-3-4.sslip.io":          true,
		"napper.useskiff.xyz":            true,
		"shop.example.org":               true,
		"xh9yvr6ula0i8yhl.useskiff.xyz":  false,
		"www.www.zzq1.dash.useskiff.xyz": false,
		"www.shop.useskiff.xyz":          false,
		"mx.1-2-3-4.sslip.io":            false,
		"other.example.org":              false,
		"useskiff.xyz.attacker.example":  false,
	} {
		if got := rt.certAllowed(host); got != want {
			t.Errorf("certAllowed(%q) = %v, want %v", host, got, want)
		}
	}
}

// A stale table is served at once: no request waits on Docker, and a failed refresh keeps the last good routes.
func TestRoutesServedWhileStale(t *testing.T) {
	rt := &Router{Engine: docker.Remote("nobody@127.0.0.1:1")}
	rt.routeTable = map[string][]int{"shop": {9000}}
	rt.routeAt = time.Now().Add(-5 * time.Second)
	start := time.Now()
	ports := rt.portsFor("shop")
	if len(ports) != 1 || ports[0] != 9000 {
		t.Fatalf("ports = %v", ports)
	}
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Fatalf("a stale lookup blocked for %v", d)
	}
}

func TestUpstreamReusesConnections(t *testing.T) {
	conns := 0
	backend := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	backend.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			conns++
		}
	}
	backend.Start()
	defer backend.Close()
	hostport := strings.TrimPrefix(backend.URL, "http://")
	for range 20 {
		w := httptest.NewRecorder()
		proxyTo(w, httptest.NewRequest("GET", "/", nil), hostport)
		if w.Code != 200 {
			t.Fatalf("status %d", w.Code)
		}
	}
	if conns != 1 {
		t.Fatalf("20 sequential requests used %d connections, want 1", conns)
	}
}

// The status address only ever reaches the panel's public status path.
func TestStatusGoesOnlyToPublicPath(t *testing.T) {
	var got []string
	panel := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = append(got, r.Method+" "+r.URL.RequestURI()) }))
	defer panel.Close()
	dir := t.TempDir()
	domains := filepath.Join(dir, "domains.json")
	_ = os.WriteFile(domains, []byte(`{"status.acme.dev":"@status:acme"}`), 0o644)
	rt := &Router{Domains: []string{"example.com"}, Panel: strings.TrimPrefix(panel.URL, "http://"), DomainsFile: domains}
	do := func(method, host, path string) int {
		req := httptest.NewRequest(method, path, nil)
		req.Host = host
		w := httptest.NewRecorder()
		rt.ServeHTTP(w, req)
		return w.Code
	}
	do("GET", "status.example.com", "/acme?x=1")
	do("GET", "status.example.com", "/")
	if c := do("GET", "status.example.com", "/../api/apps"); c != http.StatusNotFound {
		t.Errorf("a path with other characters should stop at the router, got %d", c)
	}
	do("GET", "status.acme.dev", "/api/apps")
	if c := do("POST", "status.acme.dev", "/"); c != http.StatusNotFound {
		t.Errorf("a POST should go nowhere, got %d", c)
	}
	want := []string{"GET /public/status/acme", "GET /public/status/", "GET /public/status/acme"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("panel saw %q, want %q", got, want)
	}
}

func TestPrivateAppHasNoPublicAddress(t *testing.T) {
	hit := 0
	app := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit++ }))
	defer app.Close()
	u, _ := url.Parse(app.URL)
	port, _ := strconv.Atoi(u.Port())
	dir := t.TempDir()
	rules := filepath.Join(dir, "edge.json")
	domains := filepath.Join(dir, "domains.json")
	_ = os.WriteFile(rules, []byte(`{"api":{"private":true}}`), 0o644)
	_ = os.WriteFile(domains, []byte(`{"api.acme.dev":"api","web.acme.dev":"web"}`), 0o644)
	rt := &Router{Domains: []string{"example.com"}, EdgeFile: rules, DomainsFile: domains}
	rt.routeTable = map[string][]int{"api": {port}, "web": {port}}
	rt.routeAt = time.Now().Add(time.Hour)
	get := func(host string) int {
		req := httptest.NewRequest("GET", "/", nil)
		req.Host = host
		w := httptest.NewRecorder()
		rt.ServeHTTP(w, req)
		return w.Code
	}
	if c := get("api.example.com"); c != http.StatusNotFound {
		t.Errorf("private app by subdomain: %d", c)
	}
	if c := get("api.acme.dev"); c != http.StatusNotFound {
		t.Errorf("private app by custom domain: %d", c)
	}
	if hit != 0 {
		t.Fatalf("a private app received %d public requests", hit)
	}
	if c := get("web.example.com"); c != 200 || hit != 1 {
		t.Errorf("a public app should still work: %d, %d hits", c, hit)
	}
	for host, want := range map[string]bool{"api.example.com": false, "api.acme.dev": false, "web.acme.dev": true, "web.example.com": true} {
		if got := rt.certAllowed(host); got != want {
			t.Errorf("certAllowed(%s) = %v", host, got)
		}
	}
}

func TestRateLimitThroughRouter(t *testing.T) {
	hit := 0
	app := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit++ }))
	defer app.Close()
	u, _ := url.Parse(app.URL)
	port, _ := strconv.Atoi(u.Port())
	rules := filepath.Join(t.TempDir(), "edge.json")
	_ = os.WriteFile(rules, []byte(`{"api":{"rateLimit":3}}`), 0o644)
	rt := &Router{Domains: []string{"example.com"}, EdgeFile: rules}
	rt.routeTable = map[string][]int{"api": {port}, "web": {port}}
	rt.routeAt = time.Now().Add(time.Hour)
	get := func(host, addr string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/", nil)
		req.Host, req.RemoteAddr = host, addr
		w := httptest.NewRecorder()
		rt.ServeHTTP(w, req)
		return w
	}
	for i := range 3 {
		if c := get("api.example.com", "1.1.1.1:5000").Code; c != 200 {
			t.Fatalf("request %d: %d", i, c)
		}
	}
	w := get("api.example.com", "1.1.1.1:5001")
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("over the limit: %d %v", w.Code, w.Header())
	}
	if hit != 3 {
		t.Fatalf("the app got %d requests", hit)
	}
	if c := get("api.example.com", "2.2.2.2:5000").Code; c != 200 {
		t.Errorf("another visitor shouldn't be limited: %d", c)
	}
	if c := get("web.example.com", "1.1.1.1:5000").Code; c != 200 {
		t.Errorf("another app shouldn't be limited: %d", c)
	}
}

func TestRateLimiter(t *testing.T) {
	l := newRateLimiter()
	now := time.Now()
	for i := range 60 {
		if ok, _ := l.allow("a", "v", 60, now); !ok {
			t.Fatalf("burst stopped at %d", i)
		}
	}
	ok, wait := l.allow("a", "v", 60, now)
	if ok || wait <= 0 || wait > time.Second {
		t.Fatalf("empty bucket: %v %v", ok, wait)
	}
	if ok, _ := l.allow("a", "v", 60, now.Add(1100*time.Millisecond)); !ok {
		t.Fatal("it should refill at a request a second")
	}
	if ok, _ := l.allow("a", "v", 60, now.Add(1100*time.Millisecond)); ok {
		t.Fatal("only one token should have come back")
	}
	if ok, _ := l.allow("b", "v", 60, now); !ok {
		t.Fatal("apps have separate allowances")
	}
}

func TestLimiterStaysBounded(t *testing.T) {
	l := newRateLimiter()
	now := time.Now()
	for i := range 2_000_000 {
		l.allow("a", strconv.Itoa(i), 60, now)
	}
	total := 0
	for i := range l.shards {
		if n := len(l.shards[i].visitors); n > limiterMaxShard {
			t.Fatalf("shard %d holds %d", i, n)
		}
		total += len(l.shards[i].visitors)
	}
	if total > limiterShards*limiterMaxShard {
		t.Fatalf("total %d", total)
	}
}

func TestLimitKeyGroupsIPv6(t *testing.T) {
	key := func(addr string) string {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = addr
		return limitKey(req)
	}
	if key("[2001:db8:1:2:aaaa::1]:1") != key("[2001:db8:1:2:bbbb::9]:2") {
		t.Error("the same /64 should count as one visitor")
	}
	if key("[2001:db8:1:2::1]:1") == key("[2001:db8:1:3::1]:1") {
		t.Error("different /64s are different visitors")
	}
	if key("1.2.3.4:1") != "1.2.3.4" {
		t.Errorf("ipv4 key = %q", key("1.2.3.4:1"))
	}
}

// gRPC needs HTTP/2 to the app and trailers back, and streams must arrive as they're sent.
func TestGRPCThroughRouter(t *testing.T) {
	p := new(http.Protocols)
	p.SetUnencryptedHTTP2(true)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	backend := &http.Server{Protocols: p, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/grpc")
		w.Header().Set("Trailer", "Grpc-Status")
		_, _ = w.Write([]byte("first:" + r.Proto))
		w.(http.Flusher).Flush()
		<-release
		_, _ = w.Write([]byte("|second"))
		w.Header().Set("Grpc-Status", "0")
	})}
	go backend.Serve(l)
	defer backend.Close()
	rt := &Router{Domains: []string{"example.com"}}
	rt.routeTable = map[string][]int{"svc": {l.Addr().(*net.TCPAddr).Port}}
	rt.routeAt = time.Now().Add(time.Hour)
	front := httptest.NewUnstartedServer(rt)
	front.EnableHTTP2 = true
	front.StartTLS()
	defer front.Close()

	req, _ := http.NewRequest("POST", front.URL+"/hello.Hello/Say", strings.NewReader("x"))
	req.Host = "svc.example.com"
	req.Header.Set("Content-Type", "application/grpc")
	req.Header.Set("Te", "trailers")
	resp, err := front.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 64)
	n, err := resp.Body.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "first:HTTP/2.0" {
		t.Fatalf("the first message should arrive on its own, over HTTP/2: %q", buf[:n])
	}
	close(release)
	rest, _ := io.ReadAll(resp.Body)
	if string(rest) != "|second" || resp.Trailer.Get("Grpc-Status") != "0" {
		t.Fatalf("rest %q, trailer %q", rest, resp.Trailer.Get("Grpc-Status"))
	}
}

func TestIsGRPC(t *testing.T) {
	for ct, want := range map[string]bool{
		"application/grpc":           true,
		"application/grpc+proto":     true,
		"application/grpc;charset=x": true,
		"application/grpc-web":       false,
		"application/grpc-web+proto": false,
		"application/json":           false,
		"":                           false,
	} {
		r := httptest.NewRequest("POST", "/", nil)
		r.ProtoMajor = 2
		r.Header.Set("Content-Type", ct)
		if got := isGRPC(r); got != want {
			t.Errorf("isGRPC(%q) = %v", ct, got)
		}
	}
	r := httptest.NewRequest("POST", "/", nil)
	r.Header.Set("Content-Type", "application/grpc")
	if isGRPC(r) {
		t.Error("gRPC can't arrive over HTTP/1")
	}
}
