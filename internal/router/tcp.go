package router

import (
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

const (
	tcpSyncEvery   = 2 * time.Second
	tcpDialTimeout = 5 * time.Second
	tcpMaxConns    = 10000
	tcpMaxTotal    = 20000
	tcpMaxPerIP    = 200
)

// tcpIdle is how long a forwarded connection may carry no data in either direction before it is closed.
var tcpIdle = time.Hour

// tcpProxy forwards public ports to apps' containers. The router holds the port, so a deploy's new containers take over without the address changing.
type tcpProxy struct {
	rt     *Router
	mu     sync.Mutex
	active map[int]*tcpListener
	addr   string // the address to listen on, normally every interface

	connMu sync.Mutex
	total  int
	perIP  map[string]int
}

// admit counts a new connection against the global and per-source limits.
func (t *tcpProxy) admit(key string) bool {
	t.connMu.Lock()
	defer t.connMu.Unlock()
	if t.perIP == nil {
		t.perIP = map[string]int{}
	}
	if t.total >= tcpMaxTotal || t.perIP[key] >= tcpMaxPerIP {
		return false
	}
	t.total++
	t.perIP[key]++
	return true
}

func (t *tcpProxy) release(key string) {
	t.connMu.Lock()
	defer t.connMu.Unlock()
	t.total--
	if t.perIP[key]--; t.perIP[key] <= 0 {
		delete(t.perIP, key)
	}
}

// idleConn pushes the deadline out on every transfer, so only a connection that has gone quiet is closed.
type idleConn struct{ net.Conn }

func (c idleConn) Read(b []byte) (int, error) {
	_ = c.SetDeadline(time.Now().Add(tcpIdle))
	return c.Conn.Read(b)
}

func (c idleConn) Write(b []byte) (int, error) {
	_ = c.SetDeadline(time.Now().Add(tcpIdle))
	return c.Conn.Write(b)
}

func (c idleConn) CloseWrite() error {
	if w, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return w.CloseWrite()
	}
	return nil
}

type tcpListener struct {
	app   string
	ln    net.Listener
	mu    sync.Mutex
	conns map[net.Conn]struct{}
}

func (rt *Router) startTCP() {
	if rt.EdgeFile == "" {
		return
	}
	rt.tcpOnce.Do(func() {
		rt.tcp = &tcpProxy{rt: rt, active: map[int]*tcpListener{}}
		go func() {
			for {
				rt.tcp.sync()
				time.Sleep(tcpSyncEvery)
			}
		}()
	})
}

// sync opens and closes listeners to match the rules, so changing a port needs no router restart.
func (t *tcpProxy) sync() {
	want := map[int]string{}
	for app, rule := range t.rt.edgeRules() {
		if rule.TCPPort > 0 {
			want[rule.TCPPort] = app
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for port, l := range t.active {
		if want[port] != l.app {
			l.close()
			delete(t.active, port)
		}
	}
	for port, app := range want {
		if _, open := t.active[port]; open {
			continue
		}
		ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", t.addr, port))
		if err != nil {
			log.Printf("skiff: can't open TCP port %d for %s: %v", port, app, err)
			continue
		}
		l := &tcpListener{app: app, ln: ln, conns: map[net.Conn]struct{}{}}
		t.active[port] = l
		go t.accept(l)
	}
}

func (l *tcpListener) close() {
	_ = l.ln.Close()
	l.mu.Lock()
	defer l.mu.Unlock()
	for c := range l.conns {
		_ = c.Close()
	}
}

func (l *tcpListener) track(c net.Conn) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.conns) >= tcpMaxConns {
		return false
	}
	l.conns[c] = struct{}{}
	return true
}

func (l *tcpListener) forget(c net.Conn) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.conns, c)
}

func (t *tcpProxy) accept(l *tcpListener) {
	for {
		c, err := l.ln.Accept()
		if err != nil {
			return
		}
		host, _, _ := net.SplitHostPort(c.RemoteAddr().String())
		key := limitKeyFor(host)
		if !t.admit(key) {
			_ = c.Close()
			continue
		}
		if !l.track(c) {
			t.release(key)
			_ = c.Close()
			continue
		}
		go func() {
			defer t.release(key)
			defer l.forget(c)
			t.forward(l.app, idleConn{c})
		}()
	}
}

// dial reaches one of the app's containers, starting at the next in turn, and refreshes the routes once if none answer, since a deploy may have just replaced them.
func (t *tcpProxy) dial(app string) net.Conn {
	for attempt := 0; attempt < 2; attempt++ {
		ports := t.rt.portsFor(app)
		if len(ports) == 0 {
			ports = t.rt.wake(app)
		}
		if n := len(ports); n > 0 {
			start := t.rt.nextReplica(app, n)
			for i := range n {
				up, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", ports[(start+i)%n]), tcpDialTimeout)
				if err == nil {
					return up
				}
			}
		}
		t.rt.routesMu.Lock()
		t.rt.routeAt = time.Time{}
		t.rt.routesMu.Unlock()
	}
	return nil
}

func (rt *Router) nextReplica(app string, n int) int {
	rt.rrMu.Lock()
	defer rt.rrMu.Unlock()
	if rt.rr == nil {
		rt.rr = map[string]uint64{}
	}
	i := rt.rr[app] % uint64(n)
	rt.rr[app]++
	return int(i)
}

func (t *tcpProxy) forward(app string, c net.Conn) {
	defer c.Close()
	up := t.dial(app)
	if up == nil {
		return
	}
	defer up.Close()
	up = idleConn{up}
	if addr, ok := up.RemoteAddr().(*net.TCPAddr); ok {
		defer t.rt.trackOpen(addr.Port)()
	}
	done := make(chan struct{}, 2)
	pipe := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		// Pass on one side finishing, so protocols that half-close still get their reply.
		if w, ok := dst.(idleConn); ok {
			_ = w.CloseWrite()
		}
		done <- struct{}{}
	}
	go pipe(up, c)
	go pipe(c, up)
	<-done
	<-done
}
