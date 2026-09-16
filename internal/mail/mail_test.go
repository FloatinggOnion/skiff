package mail

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"math/big"
	"net"
	"net/smtp"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"
)

type captured struct {
	from string
	to   []string
	data string
}

// sink is a minimal downstream MTA that records what it receives and can be told to refuse.
type sink struct {
	ln       net.Listener
	mu       sync.Mutex
	msgs     []captured
	rcptCode int
	dataCode int
}

func newSink(t *testing.T) *sink {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &sink{ln: ln}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(c)
		}
	}()
	return s
}

func (s *sink) serve(c net.Conn) {
	defer c.Close()
	tp := textproto.NewConn(c)
	_ = tp.PrintfLine("220 sink")
	var cur captured
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}
		verb, arg, _ := strings.Cut(line, " ")
		switch strings.ToUpper(verb) {
		case "EHLO", "HELO":
			_ = tp.PrintfLine("250-sink\r\n250 8BITMIME")
		case "MAIL":
			addr, _, _ := parsePath(arg, "FROM:")
			cur = captured{from: addr}
			_ = tp.PrintfLine("250 ok")
		case "RCPT":
			if s.rcptCode != 0 {
				_ = tp.PrintfLine("%d refused", s.rcptCode)
				continue
			}
			addr, _, _ := parsePath(arg, "TO:")
			cur.to = append(cur.to, addr)
			_ = tp.PrintfLine("250 ok")
		case "DATA":
			_ = tp.PrintfLine("354 go")
			b, err := tp.ReadDotBytes()
			if err != nil {
				return
			}
			if s.dataCode != 0 {
				_ = tp.PrintfLine("%d refused", s.dataCode)
				continue
			}
			cur.data = string(b)
			s.mu.Lock()
			s.msgs = append(s.msgs, cur)
			s.mu.Unlock()
			_ = tp.PrintfLine("250 queued")
		case "QUIT":
			_ = tp.PrintfLine("221 bye")
			return
		default:
			_ = tp.PrintfLine("250 ok")
		}
	}
}

func (s *sink) got() []captured {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]captured(nil), s.msgs...)
}

type stubResolver struct{}

func (stubResolver) LookupMX(_ context.Context, name string) ([]*net.MX, error) {
	return []*net.MX{{Host: "mx." + name + ".", Pref: 10}}, nil
}

func (stubResolver) LookupHost(context.Context, string) ([]string, error) {
	return []string{"127.0.0.1"}, nil
}

var testKey = []byte("0123456789abcdef0123456789abcdef")

func startServer(t *testing.T, rules Rules, fw *Forwarder, configure func(*Server)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	srv := &Server{Hostname: "mx.skiff.test", Rules: func() Rules { return rules }, Forwarder: fw}
	if configure != nil {
		configure(srv)
	}
	go func() { _ = srv.Serve(ln) }()
	return ln.Addr().String()
}

func directTo(s *sink) *Forwarder {
	return &Forwarder{
		Hostname: "mx.skiff.test",
		SRS:      SRS{Key: testKey},
		Resolver: stubResolver{},
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, s.ln.Addr().String())
		},
	}
}

func send(t *testing.T, addr, from string, to []string, msg string) (rcptErrs []error, dataErr error) {
	t.Helper()
	c, err := smtp.Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Hello("sender.test"); err != nil {
		t.Fatal(err)
	}
	if err := c.Mail(from); err != nil {
		t.Fatal(err)
	}
	accepted := 0
	for _, r := range to {
		err := c.Rcpt(r)
		rcptErrs = append(rcptErrs, err)
		if err == nil {
			accepted++
		}
	}
	if accepted == 0 {
		return rcptErrs, nil
	}
	w, err := c.Data()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte(msg))
	dataErr = w.Close()
	_ = c.Quit()
	return rcptErrs, dataErr
}

func code(err error) int {
	if tp, ok := err.(*textproto.Error); ok {
		return tp.Code
	}
	return 0
}

const testMsg = "From: Jane Doe <jane@origin.test>\r\nTo: hello@example.com\r\nSubject: hi\r\n\r\nhello there\r\n"

var testRules = Rules{"example.com": {"hello": {"me@dest.test"}}}

func TestForwardDirectRewritesSenderWithSRS(t *testing.T) {
	down := newSink(t)
	addr := startServer(t, testRules, directTo(down), nil)

	rcpt, err := send(t, addr, "jane@origin.test", []string{"hello@example.com"}, testMsg)
	if rcpt[0] != nil || err != nil {
		t.Fatalf("send: rcpt=%v data=%v", rcpt, err)
	}
	msgs := down.got()
	if len(msgs) != 1 {
		t.Fatalf("want 1 forwarded message, got %d", len(msgs))
	}
	m := msgs[0]
	if !strings.HasPrefix(m.from, "SRS0=") || !strings.HasSuffix(m.from, "=origin.test=jane@example.com") {
		t.Fatalf("envelope sender not SRS-rewritten onto the alias domain: %q", m.from)
	}
	if len(m.to) != 1 || m.to[0] != "me@dest.test" {
		t.Fatalf("wrong destination: %v", m.to)
	}
	if !strings.HasPrefix(m.data, "Received: from sender.test") || !strings.Contains(m.data, "From: Jane Doe <jane@origin.test>") || !strings.Contains(m.data, "hello there") {
		t.Fatalf("message not passed through intact:\n%s", m.data)
	}
}

func TestRejectsAddressesItDoesNotForward(t *testing.T) {
	down := newSink(t)
	addr := startServer(t, testRules, directTo(down), nil)

	rcpt, _ := send(t, addr, "jane@origin.test", []string{"someone@elsewhere.test", "nobody@example.com"}, testMsg)
	if code(rcpt[0]) != 550 || !strings.Contains(rcpt[0].Error(), "relaying denied") {
		t.Fatalf("foreign domain must be refused as relaying, got %v", rcpt[0])
	}
	if code(rcpt[1]) != 550 {
		t.Fatalf("unknown alias must be refused, got %v", rcpt[1])
	}
	if len(down.got()) != 0 {
		t.Fatal("nothing should have been forwarded")
	}
}

func TestLookupCatchAllAndPlusTag(t *testing.T) {
	r := Rules{"example.com": {"hello": {"a@x.test"}, "*": {"b@x.test"}}}
	cases := map[string]string{
		"hello@example.com":      "a@x.test",
		"Hello+News@Example.COM": "a@x.test",
		"random@example.com":     "b@x.test",
	}
	for in, want := range cases {
		got, ok := r.Lookup(in)
		if !ok || got[0] != want {
			t.Errorf("Lookup(%q) = %v, %v; want %s", in, got, ok, want)
		}
	}
	if _, ok := r.Lookup("hello@other.com"); ok {
		t.Error("unknown domain must not match")
	}
}

func TestResolveFollowsLocalChainsAndBreaksCycles(t *testing.T) {
	r := Rules{
		"a.test": {"team": {"ann@a.test", "bob@b.test"}, "ann": {"ann@gmail.test"}, "ping": {"pong@a.test"}, "pong": {"ping@a.test"}},
		"b.test": {"bob": {"bob@gmail.test", "ann@gmail.test"}},
	}
	if got := strings.Join(r.Resolve("team@a.test"), ","); got != "ann@gmail.test,bob@gmail.test" {
		t.Fatalf("chain not expanded locally (and deduped): %s", got)
	}
	if got := r.Resolve("ping@a.test"); len(got) != 0 {
		t.Fatalf("a forwarding cycle must resolve to nothing, got %v", got)
	}
}

func TestDownstreamFailuresMapToRetryOrReject(t *testing.T) {
	for _, tc := range []struct {
		name     string
		rcpt     int
		data     int
		wantCode int
	}{
		{"temporary rcpt", 451, 0, 451},
		{"permanent rcpt", 550, 0, 550},
		{"temporary data", 0, 452, 451},
	} {
		t.Run(tc.name, func(t *testing.T) {
			down := newSink(t)
			down.rcptCode, down.dataCode = tc.rcpt, tc.data
			addr := startServer(t, testRules, directTo(down), nil)
			_, err := send(t, addr, "jane@origin.test", []string{"hello@example.com"}, testMsg)
			if code(err) != tc.wantCode {
				t.Fatalf("want %d, got %v", tc.wantCode, err)
			}
		})
	}
}

func TestUnreachableDestinationDefers(t *testing.T) {
	fw := &Forwarder{
		SRS:      SRS{Key: testKey},
		Resolver: stubResolver{},
		Dial: func(context.Context, string, string) (net.Conn, error) {
			return nil, &net.OpError{Op: "dial", Err: net.UnknownNetworkError("refused")}
		},
	}
	addr := startServer(t, testRules, fw, nil)
	_, err := send(t, addr, "jane@origin.test", []string{"hello@example.com"}, testMsg)
	if code(err) != 451 {
		t.Fatalf("an unreachable MX must ask the sender to retry, got %v", err)
	}
}

func TestRelayRewritesFromAndKeepsReplies(t *testing.T) {
	down := newSink(t)
	host, port, _ := net.SplitHostPort(down.ln.Addr().String())
	fw := &Forwarder{SRS: SRS{Key: testKey}, Relay: &Relay{Host: host, Port: port, From: "forward@skiff.test"}}
	addr := startServer(t, testRules, fw, nil)

	if _, err := send(t, addr, "jane@origin.test", []string{"hello@example.com"}, testMsg); err != nil {
		t.Fatal(err)
	}
	msgs := down.got()
	if len(msgs) != 1 {
		t.Fatalf("want 1 relayed message, got %d", len(msgs))
	}
	m := msgs[0]
	if m.from != "forward@skiff.test" {
		t.Fatalf("relay envelope sender = %q", m.from)
	}
	if !strings.Contains(m.data, `From: "Jane Doe via hello@example.com" <forward@skiff.test>`) {
		t.Fatalf("From not rewritten onto the relay address:\n%s", m.data)
	}
	if !strings.Contains(m.data, "Reply-To: Jane Doe <jane@origin.test>") {
		t.Fatalf("replies must still reach the original sender:\n%s", m.data)
	}
	if strings.Count(m.data, "From:") != 1 {
		t.Fatalf("original From must be replaced, not duplicated:\n%s", m.data)
	}
}

func TestRewriteFromKeepsExistingReplyToAndFoldedHeaders(t *testing.T) {
	in := "Subject: a\n very long\nFrom:\n \"Ann\" <ann@x.test>\nReply-To: list@x.test\n\nbody\n"
	out := string(rewriteFrom([]byte(in), "relay@skiff.test", "hi@example.com"))
	want := "Subject: a\n very long\nFrom: \"Ann via hi@example.com\" <relay@skiff.test>\nReply-To: list@x.test\n\nbody\n"
	if out != want {
		t.Fatalf("got:\n%q\nwant:\n%q", out, want)
	}
}

func TestSRSReverseRejectsForgedAndExpired(t *testing.T) {
	s := SRS{Key: testKey}
	now := time.Now()
	fwd := s.Forward("Jane@Origin.test", "example.com", now)
	if got, ok := s.Reverse(fwd, now); !ok || !strings.EqualFold(got, "jane@origin.test") {
		t.Fatalf("round trip: %q %v", got, ok)
	}
	if got, ok := s.Reverse(strings.ToLower(fwd), now); !ok || !strings.EqualFold(got, "jane@origin.test") {
		t.Fatalf("a lowercased address must still decode: %q %v", got, ok)
	}
	if _, ok := s.Reverse(fwd, now.Add(30*24*time.Hour)); ok {
		t.Fatal("expired address accepted")
	}
	if _, ok := (SRS{Key: []byte("another-key-another-key")}).Reverse(fwd, now); ok {
		t.Fatal("address minted with another key accepted")
	}
	forged := strings.Replace(fwd, "=origin.test=", "=victim.test=", 1)
	if _, ok := s.Reverse(forged, now); ok {
		t.Fatal("forged address accepted")
	}
	if s.Forward("jane@example.com", "example.com", now) != "jane@example.com" {
		t.Fatal("a sender already on the forwarding domain should pass through")
	}
}

func TestBounceToRewrittenSenderReturnsHome(t *testing.T) {
	down := newSink(t)
	fw := directTo(down)
	addr := startServer(t, testRules, fw, nil)
	srs := fw.SRS.Forward("jane@origin.test", "example.com", time.Now())

	rcpt, err := send(t, addr, "", []string{srs, "SRS0=aaaaaa=aa=victim.test=x@example.com"}, "Subject: bounce\r\n\r\nundeliverable\r\n")
	if rcpt[0] != nil {
		t.Fatalf("valid bounce address refused: %v", rcpt[0])
	}
	if code(rcpt[1]) != 550 {
		t.Fatalf("forged bounce address must be refused, got %v", rcpt[1])
	}
	if err != nil {
		t.Fatal(err)
	}
	m := down.got()
	if len(m) != 1 || m[0].from != "" || m[0].to[0] != "jane@origin.test" {
		t.Fatalf("bounce not returned to the original sender with a null sender: %+v", m)
	}
}

func TestSizeLimitAndLoopDetection(t *testing.T) {
	down := newSink(t)
	addr := startServer(t, testRules, directTo(down), func(s *Server) { s.MaxSize = 1024 })

	_, err := send(t, addr, "jane@origin.test", []string{"hello@example.com"}, testMsg+strings.Repeat("x", 4096)+"\r\n")
	if code(err) != 552 {
		t.Fatalf("oversized message: want 552, got %v", err)
	}
	loop := strings.Repeat("Received: from a by b\r\n", maxHops) + testMsg
	_, err = send(t, addr, "jane@origin.test", []string{"hello@example.com"}, loop)
	if code(err) != 554 {
		t.Fatalf("looping message: want 554, got %v", err)
	}
	if len(down.got()) != 0 {
		t.Fatal("rejected messages must not be forwarded")
	}
}

func TestStartTLSBeforeForwarding(t *testing.T) {
	down := newSink(t)
	cert := selfSigned(t)
	addr := startServer(t, testRules, directTo(down), func(s *Server) {
		s.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
	})
	c, err := smtp.Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if ok, _ := c.Extension("STARTTLS"); !ok {
		t.Fatal("STARTTLS not advertised")
	}
	if err := c.StartTLS(&tls.Config{InsecureSkipVerify: true}); err != nil {
		t.Fatalf("StartTLS: %v", err)
	}
	if ok, _ := c.Extension("STARTTLS"); ok {
		t.Fatal("STARTTLS must not be offered again inside TLS")
	}
	if err := c.Mail("jane@origin.test"); err != nil {
		t.Fatal(err)
	}
	if err := c.Rcpt("hello@example.com"); err != nil {
		t.Fatal(err)
	}
	w, _ := c.Data()
	_, _ = w.Write([]byte(testMsg))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if m := down.got(); len(m) != 1 || !strings.Contains(m[0].data, "with ESMTPS") {
		t.Fatalf("expected one message received over TLS, got %+v", m)
	}
}

func TestRequiresHeloAndClosesAfterRepeatedErrors(t *testing.T) {
	addr := startServer(t, testRules, directTo(newSink(t)), nil)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	r := bufio.NewReader(conn)
	_, _ = r.ReadString('\n')
	_, _ = conn.Write([]byte("MAIL FROM:<a@b.test>\r\n"))
	if line, _ := r.ReadString('\n'); !strings.HasPrefix(line, "503") {
		t.Fatalf("MAIL before EHLO: %q", line)
	}
	for range maxErrors {
		_, _ = conn.Write([]byte("BOGUS\r\n"))
	}
	var last string
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			break
		}
		last = line
	}
	if !strings.HasPrefix(last, "421") {
		t.Fatalf("want the connection closed with 421, last line %q", last)
	}
}

func TestHostileInputCannotInjectIntoTheForwardedMessage(t *testing.T) {
	down := newSink(t)
	addr := startServer(t, testRules, directTo(down), nil)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	r := bufio.NewReader(conn)
	say := func(line string) string {
		_, _ = conn.Write([]byte(line + "\r\n"))
		reply, _ := r.ReadString('\n')
		return reply
	}
	_, _ = r.ReadString('\n')
	say("EHLO evil\rReceived: from trusted.test")
	for {
		line, _ := r.ReadString('\n')
		if len(line) < 4 || line[3] == ' ' {
			break
		}
	}
	if reply := say("RCPT TO:<hello\r@example.com>"); !strings.HasPrefix(reply, "503") {
		t.Fatalf("RCPT before MAIL should be refused: %q", reply)
	}
	say("MAIL FROM:<jane@origin.test>")
	if reply := say("RCPT TO:<hello\r@example.com>"); !strings.HasPrefix(reply, "501") {
		t.Fatalf("an address with a bare CR must be rejected, got %q", reply)
	}
	say("RCPT TO:<hello@example.com>")
	say("DATA")
	_, _ = conn.Write([]byte(testMsg + ".\r\n"))
	if reply, _ := r.ReadString('\n'); !strings.HasPrefix(reply, "250") {
		t.Fatalf("message not accepted: %q", reply)
	}

	msgs := down.got()
	if len(msgs) != 1 {
		t.Fatalf("want 1 forwarded message, got %d", len(msgs))
	}
	head, _, _ := strings.Cut(msgs[0].data, "\n\n")
	if strings.Count(head, "Received:") != 2 {
		t.Fatalf("a header was injected through EHLO:\n%s", head)
	}
}

func selfSigned(t *testing.T) tls.Certificate {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"mx.skiff.test"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}
