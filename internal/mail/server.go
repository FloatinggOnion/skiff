package mail

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultMaxSize = 25 << 20
	maxConns       = 64
	maxDataJobs    = 8
	maxRecipients  = 50
	maxHops        = 30
	maxErrors      = 10
	maxLine        = 4096
	maxHeaderBytes = 1 << 20
	maxHeaderLines = 1000
	cmdTimeout     = 5 * time.Minute
	sessionTimeout = 30 * time.Minute
	forwardTimeout = 3 * time.Minute
)

// Server accepts mail only for addresses its Rules know, so it can never be used as an open relay.
type Server struct {
	Hostname  string
	Rules     func() Rules
	TLSConfig *tls.Config
	Forwarder *Forwarder
	MaxSize   int64
	Logf      func(format string, args ...any)

	data chan struct{}
	lim  *limits
	once sync.Once
}

func (s *Server) limits() *limits {
	s.once.Do(func() { s.lim = newLimits() })
	return s.lim
}

func (s *Server) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}

func (s *Server) maxSize() int64 {
	if s.MaxSize > 0 {
		return s.MaxSize
	}
	return DefaultMaxSize
}

func (s *Server) ListenAndServe(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return s.Serve(ln)
}

func (s *Server) Serve(ln net.Listener) error {
	s.data = make(chan struct{}, maxDataJobs)
	conns := make(chan struct{}, maxConns)
	for {
		c, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return err
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		select {
		case conns <- struct{}{}:
			go func() {
				defer func() { <-conns }()
				host, _, _ := net.SplitHostPort(c.RemoteAddr().String())
				key := ipKey(host)
				if !s.limits().acquire(key) {
					_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
					_, _ = io.WriteString(c, "421 "+s.Hostname+" too many connections from your address\r\n")
					_ = c.Close()
					return
				}
				defer s.limits().release(key)
				s.serveConn(c)
			}()
		default:
			_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
			_, _ = io.WriteString(c, "421 "+s.Hostname+" too busy, try again later\r\n")
			_ = c.Close()
		}
	}
}

type session struct {
	s       *Server
	conn    net.Conn
	dc      *deadlineConn
	r       *bufio.Reader
	w       *bufio.Writer
	remote  string
	helo    string
	tls     bool
	hasFrom bool
	from    string
	rcpts   []Recipient
}

// deadlineConn pushes the read deadline out on every read, so a slow-but-alive sender isn't cut off mid-DATA.
type deadlineConn struct {
	net.Conn
	until time.Time
}

func (c *deadlineConn) Read(b []byte) (int, error) {
	d := time.Now().Add(cmdTimeout)
	if !c.until.IsZero() && c.until.Before(d) {
		d = c.until
	}
	_ = c.SetReadDeadline(d)
	return c.Conn.Read(b)
}

func (s *Server) serveConn(c net.Conn) {
	defer c.Close()
	kill := time.AfterFunc(sessionTimeout, func() { _ = c.Close() })
	defer kill.Stop()
	host, _, _ := net.SplitHostPort(c.RemoteAddr().String())
	ss := &session{s: s, remote: host}
	ss.attach(c)
	ss.reply(220, s.Hostname+" ESMTP ready")
	errs := 0
	for {
		line, err := ss.readLine()
		if err != nil {
			if errors.Is(err, bufio.ErrBufferFull) {
				ss.reply(500, "line too long")
				errs++
				continue
			}
			return
		}
		code := ss.handle(line)
		if code == 221 || code == -1 {
			return
		}
		if code >= 500 {
			if errs++; errs >= maxErrors {
				ss.reply(421, "too many errors, closing connection")
				return
			}
		}
	}
}

func (ss *session) attach(c net.Conn) {
	ss.conn = c
	ss.dc = &deadlineConn{Conn: c}
	ss.r = bufio.NewReaderSize(ss.dc, maxLine)
	ss.w = bufio.NewWriter(c)
}

func (ss *session) readLine() (string, error) {
	line, err := ss.r.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) {
		for errors.Is(err, bufio.ErrBufferFull) {
			_, err = ss.r.ReadSlice('\n')
		}
		if err != nil {
			return "", err
		}
		return "", bufio.ErrBufferFull
	}
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(line), "\r\n"), nil
}

func (ss *session) reply(code int, lines ...string) {
	_ = ss.conn.SetWriteDeadline(time.Now().Add(cmdTimeout))
	for i, l := range lines {
		sep := " "
		if i < len(lines)-1 {
			sep = "-"
		}
		fmt.Fprintf(ss.w, "%d%s%s\r\n", code, sep, l)
	}
	_ = ss.w.Flush()
}

func (ss *session) reset() {
	ss.hasFrom, ss.from, ss.rcpts = false, "", nil
}

// handle runs one command and returns the reply code it sent, or -1 when the connection must close.
func (ss *session) handle(line string) int {
	verb, arg, _ := strings.Cut(line, " ")
	arg = strings.TrimSpace(arg)
	switch strings.ToUpper(verb) {
	case "EHLO", "HELO":
		if arg == "" {
			ss.reply(501, "hostname required")
			return 501
		}
		ss.reset()
		ss.helo = arg
		if strings.EqualFold(verb, "HELO") {
			ss.reply(250, ss.s.Hostname)
			return 250
		}
		ext := []string{ss.s.Hostname, "PIPELINING", "8BITMIME", "SMTPUTF8", "SIZE " + strconv.FormatInt(ss.s.maxSize(), 10)}
		if ss.s.TLSConfig != nil && !ss.tls {
			ext = append(ext, "STARTTLS")
		}
		ss.reply(250, ext...)
		return 250
	case "STARTTLS":
		return ss.startTLS()
	case "MAIL":
		return ss.mail(arg)
	case "RCPT":
		return ss.rcpt(arg)
	case "DATA":
		return ss.data()
	case "RSET":
		ss.reset()
		ss.reply(250, "ok")
		return 250
	case "NOOP":
		ss.reply(250, "ok")
		return 250
	case "VRFY":
		ss.reply(252, "cannot verify, but will attempt delivery")
		return 252
	case "QUIT":
		ss.reply(221, "bye")
		return 221
	default:
		ss.reply(502, "command not implemented")
		return 502
	}
}

func (ss *session) startTLS() int {
	if ss.s.TLSConfig == nil || ss.tls {
		ss.reply(503, "TLS not available")
		return 503
	}
	// Anything already buffered was sent in plaintext and could be injected ahead of the handshake.
	if ss.r.Buffered() > 0 {
		ss.reply(554, "pipelining not allowed across STARTTLS")
		return -1
	}
	ss.reply(220, "ready to start TLS")
	tc := tls.Server(ss.conn, ss.s.TLSConfig)
	_ = tc.SetDeadline(time.Now().Add(30 * time.Second))
	if err := tc.Handshake(); err != nil {
		return -1
	}
	_ = tc.SetDeadline(time.Time{})
	ss.attach(tc)
	ss.tls, ss.helo = true, ""
	ss.reset()
	return 220
}

// parsePath pulls the address out of "FROM:<addr> PARAMS" and returns the remaining parameters.
func parsePath(arg, prefix string) (addr string, params []string, ok bool) {
	if len(arg) < len(prefix) || !strings.EqualFold(arg[:len(prefix)], prefix) {
		return "", nil, false
	}
	rest := strings.TrimSpace(arg[len(prefix):])
	if strings.HasPrefix(rest, "<") {
		end := strings.IndexByte(rest, '>')
		if end < 0 {
			return "", nil, false
		}
		addr, rest = rest[1:end], rest[end+1:]
	} else {
		addr, rest, _ = strings.Cut(rest, " ")
	}
	if i := strings.IndexByte(addr, ':'); strings.HasPrefix(addr, "@") && i >= 0 {
		addr = addr[i+1:]
	}
	if strings.ContainsAny(addr, " \t<>\r\n") {
		return "", nil, false
	}
	return addr, strings.Fields(rest), true
}

func (ss *session) mail(arg string) int {
	if ss.helo == "" {
		ss.reply(503, "send EHLO first")
		return 503
	}
	if ss.hasFrom {
		ss.reply(503, "nested MAIL command")
		return 503
	}
	addr, params, ok := parsePath(arg, "FROM:")
	if !ok || (addr != "" && !strings.Contains(addr, "@")) {
		ss.reply(501, "syntax: MAIL FROM:<address>")
		return 501
	}
	for _, p := range params {
		if k, v, _ := strings.Cut(p, "="); strings.EqualFold(k, "SIZE") {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > ss.s.maxSize() {
				ss.reply(552, "message exceeds the size limit")
				return 552
			}
		}
	}
	if !ss.s.limits().perIP.take(ipKey(ss.remote), time.Now()) {
		ss.reply(451, "too many messages, try again later")
		return 451
	}
	ss.hasFrom, ss.from = true, addr
	ss.reply(250, "ok")
	return 250
}

func (ss *session) rcpt(arg string) int {
	if !ss.hasFrom {
		ss.reply(503, "send MAIL first")
		return 503
	}
	if len(ss.rcpts) >= maxRecipients {
		ss.reply(452, "too many recipients")
		return 452
	}
	addr, _, ok := parsePath(arg, "TO:")
	_, domain, hasDomain := splitAddr(addr)
	if !ok || !hasDomain {
		ss.reply(501, "syntax: RCPT TO:<address>")
		return 501
	}
	rules := ss.s.Rules()
	if !rules.HasDomain(domain) {
		ss.reply(550, "relaying denied")
		return 550
	}
	var rc Recipient
	if IsSRS(addr) {
		orig, ok := ss.s.Forwarder.SRS.Reverse(addr, time.Now())
		if !ok {
			ss.reply(550, "no such user")
			return 550
		}
		rc = Recipient{Addr: addr, Destinations: []string{orig}, Bounce: true}
	} else {
		dst := rules.Resolve(addr)
		if len(dst) == 0 {
			ss.reply(550, "no such user")
			return 550
		}
		rc = Recipient{Addr: addr, Destinations: dst}
	}
	if !ss.s.limits().perAlias.take(strings.ToLower(addr), time.Now()) {
		ss.reply(452, "too many messages for this address, try again later")
		return 452
	}
	ss.rcpts = append(ss.rcpts, rc)
	ss.reply(250, "ok")
	return 250
}

func (ss *session) data() int {
	if !ss.hasFrom || len(ss.rcpts) == 0 {
		ss.reply(503, "send RCPT first")
		return 503
	}
	select {
	case ss.s.data <- struct{}{}:
		defer func() { <-ss.s.data }()
	case <-time.After(30 * time.Second):
		ss.reply(451, "busy, try again later")
		return 451
	}
	ss.reply(354, "end data with <CRLF>.<CRLF>")

	dr := &progressReader{r: textproto.NewReader(ss.r).DotReader(), dc: ss.dc, start: time.Now()}
	defer func() { ss.dc.until = time.Time{} }()
	var buf bytes.Buffer
	limit := ss.s.maxSize()
	if _, err := io.Copy(&buf, io.LimitReader(dr, limit+1)); err != nil {
		return -1
	}
	if int64(buf.Len()) > limit {
		if n, _ := io.Copy(io.Discard, io.LimitReader(dr, 4*limit)); n >= 4*limit {
			return -1
		}
		ss.reset()
		ss.reply(552, "message exceeds the size limit")
		return 552
	}
	defer ss.reset()
	hops, ok := countReceived(buf.Bytes())
	if !ok {
		ss.reply(552, "message headers too large")
		return 552
	}
	if hops >= maxHops {
		ss.reply(554, "too many hops, possible mail loop")
		return 554
	}

	ctx, cancel := context.WithTimeout(context.Background(), forwardTimeout)
	defer cancel()
	err := ss.s.Forwarder.Forward(ctx, Envelope{
		From:       ss.from,
		Recipients: ss.rcpts,
		Data:       append(ss.received(), buf.Bytes()...),
	})
	switch {
	case err == nil:
		ss.reply(250, "ok, forwarded")
		return 250
	case IsTemporary(err):
		ss.s.logf("mail: deferring message from %s: %v", ss.remote, err)
		ss.reply(451, "temporary delivery failure, try again later")
		return 451
	default:
		ss.s.logf("mail: rejecting message from %s: %v", ss.remote, err)
		ss.reply(550, "delivery failed: "+sanitizeReply(err.Error()))
		return 550
	}
}

func (ss *session) received() []byte {
	proto := "ESMTP"
	if ss.tls {
		proto = "ESMTPS"
	}
	return fmt.Appendf(nil, "Received: from %s (%s)\n\tby %s with %s;\n\t%s\n",
		sanitizeReply(ss.helo), ss.remote, ss.s.Hostname, proto, time.Now().UTC().Format(time.RFC1123Z))
}

// countReceived counts Received headers up to the first blank line; ok is false when the header block is too large to handle.
func countReceived(data []byte) (n int, ok bool) {
	lines := 0
	for pos := 0; pos < len(data); {
		end := bytes.IndexByte(data[pos:], '\n')
		next := len(data)
		if end >= 0 {
			end += pos
			next = end + 1
		} else {
			end = len(data)
		}
		line := bytes.TrimRight(data[pos:end], "\r")
		if len(line) == 0 {
			return n, true
		}
		if lines++; lines > maxHeaderLines || next > maxHeaderBytes {
			return n, false
		}
		if len(line) >= 9 && strings.EqualFold(string(line[:9]), "received:") {
			n++
		}
		pos = next
	}
	return n, true
}

func sanitizeReply(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
