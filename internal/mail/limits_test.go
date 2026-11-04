package mail

import (
	"bufio"
	"net"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

func TestCountReceivedStopsAtHeaderEnd(t *testing.T) {
	n, ok := countReceived([]byte("Received: a\r\nreceived: b\r\nSubject: x\r\n\r\nReceived: body\r\n"))
	if !ok || n != 2 {
		t.Fatalf("want 2 hops, got %d ok=%v", n, ok)
	}
	if _, ok := countReceived([]byte(strings.Repeat("X: y\n", maxHeaderLines+1))); ok {
		t.Fatal("too many header lines must be refused")
	}
	if _, ok := countReceived([]byte("X: " + strings.Repeat("a", maxHeaderBytes+10))); ok {
		t.Fatal("oversized header block must be refused")
	}
}

func TestHeaderFieldsKeepsFolds(t *testing.T) {
	got := headerFields("A: 1\n 2\n\t3\nB: x\n C\n D: y")
	want := []string{"A: 1\n 2\n\t3\n", "B: x\n C\n D: y"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %q", got)
	}
	if got := headerFields(" lead\nA: 1\n"); len(got) != 2 || got[0] != " lead\n" {
		t.Fatalf("leading fold: %q", got)
	}
}

func TestAdversarialMessagesAreCheap(t *testing.T) {
	size := DefaultMaxSize
	newlines := []byte(strings.Repeat("\n", size))
	folds := []byte("X: a\n" + strings.Repeat(" b\n", size/3))
	headers := []byte(strings.Repeat("A: b\n", size/5))
	start := time.Now()
	if n, ok := countReceived(newlines); !ok || n != 0 {
		t.Fatalf("newlines: %d %v", n, ok)
	}
	if _, ok := countReceived(headers); ok {
		t.Fatal("header flood must be refused")
	}
	if _, ok := countReceived(folds); ok {
		t.Fatal("fold flood must be refused")
	}
	headerFields(string(folds[:maxHeaderBytes]))
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("adversarial messages took %v", d)
	}
}

func TestOversizedHeadersRefused(t *testing.T) {
	down := newSink(t)
	addr := startServer(t, testRules, directTo(down), nil)
	_, err := send(t, addr, "jane@origin.test", []string{"hello@example.com"}, strings.Repeat("X-A: b\r\n", maxHeaderLines+1)+"\r\nbody\r\n")
	if code(err) != 552 {
		t.Fatalf("want 552, got %v", err)
	}
	if len(down.got()) != 0 {
		t.Fatal("refused message must not be forwarded")
	}
}

func TestSlowDataIsCutOff(t *testing.T) {
	old := dataGrace
	dataGrace = 200 * time.Millisecond
	defer func() { dataGrace = old }()
	addr := startServer(t, testRules, directTo(newSink(t)), nil)
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	tp := textproto.NewConn(c)
	_, _, _ = tp.ReadResponse(220)
	for _, l := range []string{"EHLO x", "MAIL FROM:<a@b.test>", "RCPT TO:<hello@example.com>", "DATA"} {
		_ = tp.PrintfLine("%s", l)
		_, _, _ = tp.ReadResponse(0)
	}
	_ = tp.PrintfLine("Subject: slow")
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, err = c.Read(make([]byte, 1))
	if err == nil {
		t.Fatal("expected connection closed")
	}
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatal("stalled DATA connection was not cut off")
	}
}

func TestPerIPConnectionCap(t *testing.T) {
	addr := startServer(t, testRules, directTo(newSink(t)), nil)
	for i := 0; i < maxConnsPerIP; i++ {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		if l, _ := bufio.NewReader(c).ReadString('\n'); !strings.HasPrefix(l, "220") {
			t.Fatalf("conn %d: %q", i, l)
		}
	}
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if l, _ := bufio.NewReader(c).ReadString('\n'); !strings.HasPrefix(l, "421") {
		t.Fatalf("over the cap: %q", l)
	}
}

func TestMessageCounterWindow(t *testing.T) {
	cn := newCounter(2)
	now := time.Now()
	if !cn.take("a", now) || !cn.take("a", now) || cn.take("a", now) {
		t.Fatal("window should allow exactly 2")
	}
	if !cn.take("a", now.Add(rateWindow)) {
		t.Fatal("window should reset")
	}
	if ipKey("2001:db8::1") != ipKey("2001:db8::2") || ipKey("1.2.3.4") != "1.2.3.4" {
		t.Fatal("ipv6 should group by /64")
	}
}
