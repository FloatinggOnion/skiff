package mail

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	netmail "net/mail"
	"net/smtp"
	"net/textproto"
	"os"
	"strings"
	"time"
)

// Relay is an upstream SMTP server that sends on our behalf, for hosts that can't reach port 25 directly.
type Relay struct {
	Host, Port, User, Pass, From string
}

func RelayFromEnv() *Relay {
	host, from := os.Getenv("SKIFF_SMTP_HOST"), os.Getenv("SKIFF_SMTP_FROM")
	if host == "" || from == "" {
		return nil
	}
	port := os.Getenv("SKIFF_SMTP_PORT")
	if port == "" {
		port = "587"
	}
	return &Relay{Host: host, Port: port, User: os.Getenv("SKIFF_SMTP_USER"), Pass: os.Getenv("SKIFF_SMTP_PASS"), From: from}
}

type Resolver interface {
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
	LookupHost(ctx context.Context, host string) ([]string, error)
}

type Forwarder struct {
	Hostname string
	SRS      SRS
	Relay    *Relay
	Resolver Resolver
	Dial     func(ctx context.Context, network, addr string) (net.Conn, error)
	Port     string
	Logf     func(format string, args ...any)
}

type Recipient struct {
	Addr         string
	Destinations []string
	Bounce       bool // addressed to a rewritten sender, so it goes back out with a null sender
}

type Envelope struct {
	From       string
	Recipients []Recipient
	Data       []byte
}

// Error carries an SMTP-shaped outcome: Temporary asks the sender to retry later instead of bouncing.
type Error struct {
	Temporary bool
	Msg       string
}

func (e *Error) Error() string { return e.Msg }

func temporary(err error) error { return &Error{Temporary: true, Msg: err.Error()} }

func classify(err error) error {
	var tp *textproto.Error
	if errors.As(err, &tp) {
		return &Error{Temporary: tp.Code < 500, Msg: fmt.Sprintf("%d %s", tp.Code, tp.Msg)}
	}
	var me *Error
	if errors.As(err, &me) {
		return me
	}
	return temporary(err)
}

func IsTemporary(err error) bool {
	var me *Error
	return !errors.As(err, &me) || me.Temporary
}

type job struct {
	from   string
	domain string
	to     []string
	data   []byte
}

func (f *Forwarder) logf(format string, args ...any) {
	if f.Logf != nil {
		f.Logf(format, args...)
	}
}

// Forward delivers env to every destination; once any copy lands it reports success, since a retry would duplicate it for those that already got it.
func (f *Forwarder) Forward(ctx context.Context, env Envelope) error {
	var delivered int
	var failed error
	for _, j := range f.plan(env, time.Now()) {
		if err := f.send(ctx, j); err != nil {
			f.logf("mail: forward to %s failed: %v", strings.Join(j.to, ","), err)
			if failed == nil || IsTemporary(err) {
				failed = err
			}
			continue
		}
		delivered++
	}
	if delivered > 0 {
		return nil
	}
	return failed
}

func (f *Forwarder) plan(env Envelope, now time.Time) []job {
	seen := map[string]bool{}
	groups := map[string]*job{}
	var order []string
	for _, rc := range env.Recipients {
		_, aliasDomain, _ := splitAddr(rc.Addr)
		from := env.From
		switch {
		case f.Relay != nil:
			from = f.Relay.From
		case rc.Bounce:
			from = ""
		case from != "":
			from = f.SRS.Forward(from, aliasDomain, now)
		}
		for _, dst := range rc.Destinations {
			key := strings.ToLower(dst)
			if seen[key] {
				continue
			}
			seen[key] = true
			_, domain, ok := splitAddr(dst)
			if !ok {
				continue
			}
			if f.Relay != nil {
				domain = ""
			}
			gk := from + "\x00" + domain
			g, ok := groups[gk]
			if !ok {
				data := env.Data
				if f.Relay != nil {
					data = rewriteFrom(env.Data, f.Relay.From, rc.Addr)
				}
				g = &job{from: from, domain: domain, data: data}
				groups[gk] = g
				order = append(order, gk)
			}
			g.to = append(g.to, dst)
		}
	}
	jobs := make([]job, 0, len(order))
	for _, k := range order {
		jobs = append(jobs, *groups[k])
	}
	return jobs
}

func (f *Forwarder) send(ctx context.Context, j job) error {
	if r := f.Relay; r != nil {
		var auth smtp.Auth
		if r.User != "" {
			auth = smtp.PlainAuth("", r.User, r.Pass, r.Host)
		}
		return f.transact(ctx, net.JoinHostPort(r.Host, r.Port), r.Host, r.Port == "465", true, auth, j)
	}
	hosts, err := f.exchangers(ctx, j.domain)
	if err != nil {
		return err
	}
	port := f.Port
	if port == "" {
		port = "25"
	}
	var last error
	for _, h := range hosts {
		err := f.transact(ctx, net.JoinHostPort(h, port), h, false, false, nil, j)
		if err == nil {
			return nil
		}
		last = err
		if !IsTemporary(err) {
			return err
		}
	}
	return last
}

func (f *Forwarder) resolver() Resolver {
	if f.Resolver != nil {
		return f.Resolver
	}
	return net.DefaultResolver
}

// exchangers lists a domain's mail hosts in preference order, falling back to the domain itself when it publishes no MX.
func (f *Forwarder) exchangers(ctx context.Context, domain string) ([]string, error) {
	lctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	mxs, err := f.resolver().LookupMX(lctx, domain)
	var dnsErr *net.DNSError
	if err != nil && !(errors.As(err, &dnsErr) && dnsErr.IsNotFound) {
		return nil, temporary(err)
	}
	if len(mxs) == 1 && (mxs[0].Host == "." || mxs[0].Host == "") {
		return nil, &Error{Msg: domain + " does not accept mail"}
	}
	var hosts []string
	for _, mx := range mxs {
		hosts = append(hosts, strings.TrimSuffix(mx.Host, "."))
	}
	if len(hosts) > 0 {
		return hosts, nil
	}
	if _, err := f.resolver().LookupHost(lctx, domain); err != nil {
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return nil, &Error{Msg: "no mail server found for " + domain}
		}
		return nil, temporary(err)
	}
	return []string{domain}, nil
}

func (f *Forwarder) dial(ctx context.Context, addr string) (net.Conn, error) {
	dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if f.Dial != nil {
		return f.Dial(dctx, "tcp", addr)
	}
	var d net.Dialer
	return d.DialContext(dctx, "tcp", addr)
}

func (f *Forwarder) transact(ctx context.Context, addr, serverName string, implicitTLS, verify bool, auth smtp.Auth, j job) error {
	conn, err := f.dial(ctx, addr)
	if err != nil {
		return temporary(err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	// Receiving MXs routinely present certificates for some other name; unverified encryption still beats plaintext.
	tlsCfg := &tls.Config{ServerName: serverName, InsecureSkipVerify: !verify, MinVersion: tls.VersionTLS12}
	if implicitTLS {
		tc := tls.Client(conn, tlsCfg)
		if err := tc.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return temporary(err)
		}
		conn = tc
	}
	c, err := smtp.NewClient(conn, serverName)
	if err != nil {
		_ = conn.Close()
		return classify(err)
	}
	defer c.Close()
	hostname := f.Hostname
	if hostname == "" {
		hostname = "localhost"
	}
	if err := c.Hello(hostname); err != nil {
		return classify(err)
	}
	if ok, _ := c.Extension("STARTTLS"); ok && !implicitTLS {
		if err := c.StartTLS(tlsCfg); err != nil {
			return classify(err)
		}
	}
	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return classify(err)
		}
	}
	if err := c.Mail(j.from); err != nil {
		return classify(err)
	}
	var accepted int
	var rejected error
	for _, to := range j.to {
		if err := c.Rcpt(to); err != nil {
			f.logf("mail: %s refused %s: %v", serverName, to, err)
			rejected = classify(err)
			continue
		}
		accepted++
	}
	if accepted == 0 {
		return rejected
	}
	w, err := c.Data()
	if err != nil {
		return classify(err)
	}
	if _, err := w.Write(j.data); err != nil {
		return classify(err)
	}
	if err := w.Close(); err != nil {
		return classify(err)
	}
	_ = c.Quit()
	return nil
}

// rewriteFrom puts the relay's own address in From (relays refuse to send as arbitrary domains) and keeps replies going to the original sender.
func rewriteFrom(data []byte, relayFrom, alias string) []byte {
	head, body, sep := splitMessage(data)
	fields := headerFields(head)
	var orig string
	hasReplyTo := false
	for _, fl := range fields {
		switch fieldName(fl) {
		case "from":
			orig = fieldValue(fl)
		case "reply-to":
			hasReplyTo = true
		}
	}
	name := alias
	if a, err := netmail.ParseAddress(orig); err == nil {
		name = a.Address
		if a.Name != "" {
			name = a.Name
		}
		name += " via " + alias
	}
	from := (&netmail.Address{Name: name, Address: relayFrom}).String()

	var out bytes.Buffer
	for _, fl := range fields {
		if fieldName(fl) == "from" {
			out.WriteString("From: " + from + "\n")
			if !hasReplyTo && orig != "" {
				out.WriteString("Reply-To:" + strings.TrimRight(fl[strings.IndexByte(fl, ':')+1:], "\r\n") + "\n")
			}
			continue
		}
		out.WriteString(fl)
	}
	if orig == "" {
		out.WriteString("From: " + from + "\n")
	}
	out.WriteString(sep)
	out.Write(body)
	return out.Bytes()
}

func splitMessage(data []byte) (head string, body []byte, sep string) {
	best, s := -1, ""
	for _, cand := range []string{"\r\n\r\n", "\n\n"} {
		if i := bytes.Index(data, []byte(cand)); i >= 0 && (best < 0 || i < best) {
			best, s = i, cand
		}
	}
	if best < 0 {
		return string(data), nil, "\n"
	}
	n := len(s) / 2
	return string(data[:best+n]), data[best+len(s):], s[n:]
}

// headerFields splits a header block into whole fields, each keeping its folded continuation lines.
func headerFields(head string) []string {
	var fields []string
	start, begun := 0, false
	for pos := 0; pos < len(head); {
		next := len(head)
		if i := strings.IndexByte(head[pos:], '\n'); i >= 0 {
			next = pos + i + 1
		}
		if c := head[pos]; !begun || (c != ' ' && c != '\t') {
			if begun {
				fields = append(fields, head[start:pos])
			}
			start, begun = pos, true
		}
		pos = next
	}
	if begun {
		fields = append(fields, head[start:])
	}
	return fields
}

func fieldName(field string) string {
	i := strings.IndexByte(field, ':')
	if i < 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(field[:i]))
}

func fieldValue(field string) string {
	i := strings.IndexByte(field, ':')
	if i < 0 {
		return ""
	}
	return strings.Join(strings.Fields(field[i+1:]), " ")
}
