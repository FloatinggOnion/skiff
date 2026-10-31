package router

import (
	"net/http"
	"net/http/httptest"
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
