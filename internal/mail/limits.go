package mail

import (
	"io"
	"net"
	"sync"
	"time"
)

const (
	maxConnsPerIP    = 6
	maxMsgsPerIP     = 300
	maxMsgsPerAlias  = 600
	rateWindow       = time.Hour
	maxTracked       = 50_000
	dataTimeout      = 10 * time.Minute
	dataMinBytesPerS = 32 << 10
)

var dataGrace = 30 * time.Second

// counter is a fixed-window count per key, bounded by dropping old windows first.
type counter struct {
	mu    sync.Mutex
	limit int
	seen  map[string]*window
}

type window struct {
	n     int
	start time.Time
}

func newCounter(limit int) *counter {
	return &counter{limit: limit, seen: map[string]*window{}}
}

// take reports whether key is still under its limit in the current window, counting this call.
func (c *counter) take(key string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	w := c.seen[key]
	if w == nil || now.Sub(w.start) >= rateWindow {
		if w == nil && len(c.seen) >= maxTracked {
			c.evict(now)
		}
		w = &window{start: now}
		c.seen[key] = w
	}
	w.n++
	return w.n <= c.limit
}

func (c *counter) evict(now time.Time) {
	for k, w := range c.seen {
		if now.Sub(w.start) >= rateWindow/2 {
			delete(c.seen, k)
		}
	}
	for k := range c.seen {
		if len(c.seen) < maxTracked {
			return
		}
		delete(c.seen, k)
	}
}

type limits struct {
	perIP, perAlias *counter
	mu              sync.Mutex
	conns           map[string]int
}

func newLimits() *limits {
	return &limits{perIP: newCounter(maxMsgsPerIP), perAlias: newCounter(maxMsgsPerAlias), conns: map[string]int{}}
}

// ipKey groups an IPv6 client by its /64, which is what one subscriber controls.
func ipKey(host string) string {
	ip := net.ParseIP(host)
	if ip == nil {
		return host
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String()
}

func (l *limits) acquire(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conns[key] >= maxConnsPerIP {
		return false
	}
	l.conns[key]++
	return true
}

func (l *limits) release(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conns[key]--; l.conns[key] <= 0 {
		delete(l.conns, key)
	}
}

// progressReader lets a DATA transfer run only as long as it keeps up a minimum rate, and never past dataTimeout.
type progressReader struct {
	r     io.Reader
	dc    *deadlineConn
	start time.Time
	n     int64
}

func (p *progressReader) Read(b []byte) (int, error) {
	allow := dataGrace + time.Duration(p.n)*time.Second/dataMinBytesPerS
	if allow > dataTimeout {
		allow = dataTimeout
	}
	p.dc.until = p.start.Add(allow)
	n, err := p.r.Read(b)
	p.n += int64(n)
	return n, err
}
