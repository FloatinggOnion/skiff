package router

import (
	"hash/fnv"
	"math"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	limiterShards   = 32
	limiterMaxShard = 20_000 // visitors tracked per shard before old ones are dropped
)

type bucket struct {
	tokens float64
	last   time.Time
}

type limiterShard struct {
	sync.Mutex
	visitors map[string]*bucket
}

// rateLimiter is a token bucket per app and visitor: a burst of a full minute's allowance, refilling steadily.
type rateLimiter struct{ shards [limiterShards]limiterShard }

func newRateLimiter() *rateLimiter {
	l := &rateLimiter{}
	for i := range l.shards {
		l.shards[i].visitors = map[string]*bucket{}
	}
	return l
}

// allow reports whether a request may pass, and otherwise how long until one could.
func (l *rateLimiter) allow(app, visitor string, perMinute int, now time.Time) (bool, time.Duration) {
	key := app + "|" + visitor
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	s := &l.shards[h.Sum32()%limiterShards]
	s.Lock()
	defer s.Unlock()
	if len(s.visitors) >= limiterMaxShard {
		s.sweep(now)
	}
	b := s.visitors[key]
	limit := float64(perMinute)
	if b == nil {
		b = &bucket{tokens: limit, last: now}
		s.visitors[key] = b
	}
	b.tokens = math.Min(limit, b.tokens+now.Sub(b.last).Seconds()*limit/60)
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) * 60 / limit * float64(time.Second))
}

// sweep drops visitors idle long enough to be back at full allowance, and everyone if that frees nothing, which only forgets a little history under a flood of addresses.
func (s *limiterShard) sweep(now time.Time) {
	for k, b := range s.visitors {
		if now.Sub(b.last) > 2*time.Minute {
			delete(s.visitors, k)
		}
	}
	if len(s.visitors) >= limiterMaxShard {
		s.visitors = map[string]*bucket{}
	}
}

// limitKey identifies who a request counts against. An IPv6 client controls a whole /64, so that's what counts as one visitor.
func limitKey(r *http.Request) string {
	ip := net.ParseIP(clientIP(r))
	if ip == nil {
		return clientIP(r)
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String()
}

func tooManyRequests(w http.ResponseWriter, wait time.Duration) {
	secs := int(math.Ceil(wait.Seconds()))
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	http.Error(w, "too many requests", http.StatusTooManyRequests)
}
