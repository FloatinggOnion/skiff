package router

import (
	"fmt"
	"net"
	"testing"
	"time"
)

func TestTCPConnectionCaps(t *testing.T) {
	p := &tcpProxy{}
	for i := 0; i < tcpMaxPerIP; i++ {
		if !p.admit("1.1.1.1") {
			t.Fatalf("connection %d refused early", i)
		}
	}
	if p.admit("1.1.1.1") {
		t.Fatal("one source should not exceed its cap")
	}
	if !p.admit("2.2.2.2") {
		t.Fatal("another source should be unaffected")
	}
	p.release("1.1.1.1")
	if !p.admit("1.1.1.1") {
		t.Fatal("a released slot should be reusable")
	}

	g := &tcpProxy{}
	for i := 0; i < tcpMaxTotal; i++ {
		if !g.admit(fmt.Sprintf("10.0.%d.%d", i/200, i%200)) {
			t.Fatalf("connection %d refused early", i)
		}
	}
	if g.admit("203.0.113.1") {
		t.Fatal("the global cap should hold across sources")
	}
}

func TestIdleConnClosesOnlyQuietConnections(t *testing.T) {
	old := tcpIdle
	tcpIdle = 200 * time.Millisecond
	defer func() { tcpIdle = old }()

	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	c := idleConn{a}
	go func() {
		for i := 0; i < 5; i++ {
			time.Sleep(100 * time.Millisecond)
			_, _ = b.Write([]byte("x"))
		}
	}()
	buf := make([]byte, 1)
	for i := 0; i < 5; i++ {
		if _, err := c.Read(buf); err != nil {
			t.Fatalf("an active connection was cut after %d reads: %v", i, err)
		}
	}
	start := time.Now()
	if _, err := c.Read(buf); err == nil || time.Since(start) > time.Second {
		t.Fatalf("a quiet connection should time out promptly: %v after %v", err, time.Since(start))
	}
}
