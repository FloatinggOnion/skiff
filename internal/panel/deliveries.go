package panel

import (
	"sync"
	"time"
)

const deliveryRepeatWindow = 5 * time.Minute

// recentDeliveries remembers recent webhook delivery ids, so a captured request that's sent straight back does nothing while a person can still press Redeliver later. Bounded: the oldest id is forgotten first.
type recentDeliveries struct {
	mu   sync.Mutex
	seen map[string]time.Time
	ring []string
	next int
}

func newRecentDeliveries(n int) *recentDeliveries {
	return &recentDeliveries{seen: make(map[string]time.Time, n), ring: make([]string, n)}
}

// firstSeen records id and reports whether it's new. An empty id is never a repeat.
func (d *recentDeliveries) firstSeen(id string) bool {
	return d.firstSeenAt(id, time.Now())
}

func (d *recentDeliveries) firstSeenAt(id string, now time.Time) bool {
	if id == "" {
		return true
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if at, ok := d.seen[id]; ok {
		if now.Sub(at) < deliveryRepeatWindow {
			return false
		}
		d.seen[id] = now
		return true
	}
	if old := d.ring[d.next]; old != "" {
		delete(d.seen, old)
	}
	d.ring[d.next] = id
	d.seen[id] = now
	d.next = (d.next + 1) % len(d.ring)
	return true
}

var hookDeliveries = newRecentDeliveries(2048)
