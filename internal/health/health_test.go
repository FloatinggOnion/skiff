package health

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func serve(t *testing.T, h http.HandlerFunc) (string, int) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	addr := srv.Listener.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port
}

func TestWait(t *testing.T) {
	host, port := serve(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		case "/moved":
			http.Redirect(w, r, "/nowhere", http.StatusFound)
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	})
	if err := Wait(host, port, "", time.Second); err != nil {
		t.Errorf("without a path any reply should do: %v", err)
	}
	if err := Wait(host, port, "/healthz", time.Second); err != nil {
		t.Errorf("200 should pass: %v", err)
	}
	if err := Wait(host, port, "/moved", time.Second); err != nil {
		t.Errorf("a redirect should pass without being followed: %v", err)
	}
	err := Wait(host, port, "/broken", time.Second)
	if err == nil || !strings.Contains(err.Error(), "GET /broken answered 503") {
		t.Errorf("503 should fail with the status, got %v", err)
	}
}

func TestWaitNoListener(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	if Wait("127.0.0.1", port, "", 700*time.Millisecond) == nil {
		t.Error("a closed port can't be healthy")
	}
}
