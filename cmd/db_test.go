package cmd

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func tunnelServer(t *testing.T, h http.HandlerFunc) (credentials, func(context.Context, net.Listener)) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := credentials{URL: srv.URL, Token: "secret"}
	return c, func(ctx context.Context, l net.Listener) { serveTunnel(ctx, l, c, "orders", io.Discard) }
}

func TestTunnelCarriesLargeMessagesBothWays(t *testing.T) {
	_, serve := tunnelServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/databases/orders/tunnel" || r.Header.Get("Authorization") != "Bearer secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		ws.SetReadLimit(-1)
		peer := websocket.NetConn(context.Background(), ws, websocket.MessageBinary)
		_, _ = io.Copy(peer, peer)
	})
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { serve(ctx, l); close(done) }()

	conn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	want := bytes.Repeat([]byte("skiff"), 200_000) // far past a WebSocket's default 32 KiB read limit
	go func() { _, _ = conn.Write(want) }()
	got := make([]byte, len(want))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("bytes changed in the tunnel")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("closing the tunnel left it running")
	}
}

func TestTunnelExplainsARefusedConnection(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		want   string
	}{
		{http.StatusUnauthorized, `{"error":"invalid or missing API token"}`, "skiff login"},
		{http.StatusConflict, `{"error":"orders isn't running"}`, "orders isn't running"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		a, b := net.Pipe()
		err := carryConnection(context.Background(), a, strings.Replace(srv.URL, "http", "ws", 1), "t")
		srv.Close()
		a.Close()
		b.Close()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("status %d: got %v, want it to mention %q", tc.status, err, tc.want)
		}
	}
}

func TestListenLocalStaysOnLoopback(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	taken := busy.Addr().(*net.TCPAddr).Port

	if _, err := listenLocal(taken, 0); err == nil {
		t.Error("a port the user chose was busy, and that should be an error")
	}
	l, err := listenLocal(0, taken)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	addr := l.Addr().(*net.TCPAddr)
	if addr.Port == taken || !addr.IP.IsLoopback() {
		t.Errorf("fell back to %s", addr)
	}
}

func TestListDatabasesTellsAnOldServerFromAnEmptyTeam(t *testing.T) {
	for _, tc := range []struct {
		body    string
		wantErr bool
	}{
		{"<!doctype html><html></html>", true},
		{"[]", false},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(tc.body))
		}))
		_, err := listRemoteDatabases(credentials{URL: srv.URL, Token: "t"})
		srv.Close()
		if (err != nil) != tc.wantErr {
			t.Errorf("body %q: got error %v", tc.body, err)
		}
	}
}
