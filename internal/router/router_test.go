package router

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	rt.cachedRoutes = []docker.Route{{App: "shop", HostPort: 1}}
	rt.cachedRouteAt = time.Now().Add(time.Hour)

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
	rt.cachedRoutes = []docker.Route{{App: "shop", HostPort: 1}}
	rt.cachedRouteAt = time.Now().Add(time.Hour)

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
