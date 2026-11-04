package proxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOnlyLocalhostNamesAreServed(t *testing.T) {
	for host, want := range map[string]bool{
		"myapp.localhost":      true,
		"myapp.localhost:8080": true,
		"MyApp.Localhost.":     true,
		"localhost:8080":       true,
		"evil.example":         false,
		"myapp.evil.example":   false,
		"localhost.evil.test":  false,
		"127.0.0.1:8080":       false,
		"":                     false,
	} {
		if got := localHost(host); got != want {
			t.Errorf("localHost(%q) = %v, want %v", host, got, want)
		}
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "myapp.evil.example"
	w := httptest.NewRecorder()
	New().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("foreign host: %d", w.Code)
	}
}

func TestDefaultBindsLoopbackAndKeepsURL(t *testing.T) {
	if !strings.HasPrefix(DefaultAddr, "127.0.0.1:") {
		t.Fatalf("default address %q is not loopback", DefaultAddr)
	}
	if got := URL("app"); got != "http://app.localhost:8080" {
		t.Fatalf("URL = %q", got)
	}
}
