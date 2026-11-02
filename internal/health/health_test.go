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

// A gRPC server only speaks plain-text HTTP/2, and answers an HTTP/1 request with nothing useful.
func TestWaitAcceptsHTTP2Only(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := new(http.Protocols)
	p.SetUnencryptedHTTP2(true)
	srv := &http.Server{Protocols: p, Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})}
	go srv.Serve(l)
	defer srv.Close()
	port := l.Addr().(*net.TCPAddr).Port
	if err := Wait("127.0.0.1", port, "", 3*time.Second); err != nil {
		t.Fatalf("an HTTP/2-only server should pass: %v", err)
	}
	// A declared path is a real check over HTTP/1; it isn't waved through.
	if err := Wait("127.0.0.1", port, "/healthz", 1500*time.Millisecond); err == nil {
		t.Fatal("a path must be answered properly")
	}
}

// Accepting a connection and hanging up, as docker's proxy does when the app isn't up yet, isn't healthy.
func TestWaitRejectsSilentClose(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	if err := Wait("127.0.0.1", l.Addr().(*net.TCPAddr).Port, "", 1500*time.Millisecond); err == nil {
		t.Fatal("a port that hangs up isn't a healthy app")
	}
}

// An HTTP/1 server that isn't HTTP/2 doesn't pass the handshake.
func TestSpeaksHTTP2(t *testing.T) {
	host, port := serve(t, func(http.ResponseWriter, *http.Request) {})
	if speaksHTTP2(host, port) {
		t.Fatal("an HTTP/1 server claimed to speak HTTP/2")
	}
	if speaksHTTP2("127.0.0.1", 1) {
		t.Fatal("a closed port claimed to speak HTTP/2")
	}
}
