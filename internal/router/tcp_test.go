package router

import (
	"bufio"
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/nuelScript/skiff/internal/docker"
	"github.com/nuelScript/skiff/internal/edge"
)

func echoServer(t *testing.T, greeting string) (int, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				r := bufio.NewReader(c)
				line, err := r.ReadString('\n')
				if err != nil {
					return
				}
				fmt.Fprintf(c, "%s %s", greeting, line)
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, func() { ln.Close() }
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func tcpAsk(port int) (string, error) {
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		return "", err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	fmt.Fprintln(c, "hello")
	return bufio.NewReader(c).ReadString('\n')
}

func TestTCPProxy(t *testing.T) {
	first, stopFirst := echoServer(t, "first")
	second, stopSecond := echoServer(t, "second")
	defer stopSecond()
	file := filepath.Join(t.TempDir(), "edge.json")
	public := freePort(t)
	if err := edge.Save(file, edge.Rules{"db": {TCPPort: public}}); err != nil {
		t.Fatal(err)
	}
	rt := &Router{EdgeFile: file, Engine: docker.Local(), routeTable: map[string][]int{"db": {first}}, routeAt: time.Now().Add(time.Hour)}
	proxy := &tcpProxy{rt: rt, active: map[int]*tcpListener{}, addr: "127.0.0.1"}
	defer func() {
		for _, l := range proxy.active {
			l.close()
		}
	}()
	proxy.sync()

	if got, err := tcpAsk(public); err != nil || got != "first hello\n" {
		t.Fatalf("forwarding: %q %v", got, err)
	}

	// A new container takes over on the same public port, as in a deploy.
	rt.routesMu.Lock()
	rt.routeTable = map[string][]int{"db": {second}}
	rt.routesMu.Unlock()
	stopFirst()
	if got, err := tcpAsk(public); err != nil || got != "second hello\n" {
		t.Fatalf("after a deploy: %q %v", got, err)
	}

	// A connection with nothing to reach closes rather than hangs.
	if err := edge.Save(file, edge.Rules{"gone": {TCPPort: public}}); err != nil {
		t.Fatal(err)
	}
	rt.edgeMu.Lock()
	rt.cachedEdgeAt = time.Time{}
	rt.edgeMu.Unlock()
	proxy.sync()
	rt.fetchRoutesForTest(map[string][]int{"gone": {freePort(t)}})
	if got, err := tcpAsk(public); err == nil && got != "" {
		t.Fatalf("no backend should answer: %q", got)
	}

	// Removing the rule closes the port, without a restart.
	if err := edge.Save(file, edge.Rules{}); err != nil {
		t.Fatal(err)
	}
	rt.edgeMu.Lock()
	rt.cachedEdgeAt = time.Time{}
	rt.edgeMu.Unlock()
	proxy.sync()
	if _, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", public), 300*time.Millisecond); err == nil {
		t.Fatal("the port should be closed")
	}
}

func (rt *Router) fetchRoutesForTest(table map[string][]int) {
	rt.routesMu.Lock()
	defer rt.routesMu.Unlock()
	rt.routeTable, rt.routeAt = table, time.Now().Add(time.Hour)
}
