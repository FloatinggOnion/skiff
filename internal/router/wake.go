package router

import (
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/nuelScript/skiff/internal/sleep"
)

var wakeTimeout = 30 * time.Second

func (rt *Router) sleepingApps() sleep.Apps {
	if rt.SleepFile == "" {
		return nil
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.cachedSleep != nil && time.Since(rt.cachedSleepAt) < 2*time.Second {
		return rt.cachedSleep
	}
	rt.cachedSleep, rt.cachedSleepAt = sleep.Load(rt.SleepFile), time.Now()
	return rt.cachedSleep
}

const (
	maxWakeWaiters = 32
	wakeFailTTL    = 10 * time.Second
)

// wakeState serializes wakes of one app, so a stuck app only holds up its own visitors.
type wakeState struct {
	run      sync.Mutex
	waiters  int
	failedAt time.Time
}

func (rt *Router) wakeStateFor(app string) *wakeState {
	rt.wakeMu.Lock()
	defer rt.wakeMu.Unlock()
	if rt.wakes == nil {
		rt.wakes = map[string]*wakeState{}
	}
	st := rt.wakes[app]
	if st == nil {
		st = &wakeState{}
		rt.wakes[app] = st
	}
	return st
}

// enter reserves a place in the queue, refusing when the app just failed to wake or too many visitors are already waiting.
func (rt *Router) enterWake(st *wakeState) bool {
	rt.wakeMu.Lock()
	defer rt.wakeMu.Unlock()
	if time.Since(st.failedAt) < wakeFailTTL || st.waiters >= maxWakeWaiters {
		return false
	}
	st.waiters++
	return true
}

func (rt *Router) leaveWake(st *wakeState, failed bool) {
	rt.wakeMu.Lock()
	defer rt.wakeMu.Unlock()
	st.waiters--
	if failed {
		st.failedAt = time.Now()
	}
}

func (rt *Router) wakeFailed(st *wakeState) bool {
	rt.wakeMu.Lock()
	defer rt.wakeMu.Unlock()
	return time.Since(st.failedAt) < wakeFailTTL
}

// wake starts a sleeping app's containers and waits until one accepts connections, returning its ports; the request that woke it is held until then.
func (rt *Router) wake(app string) []int {
	containers := rt.sleepingApps()[app]
	if len(containers) == 0 {
		return nil
	}
	st := rt.wakeStateFor(app)
	if !rt.enterWake(st) {
		return nil
	}
	failed := false
	defer func() { rt.leaveWake(st, failed) }()
	st.run.Lock()
	defer st.run.Unlock()
	// Whoever held the lock before us may have brought it up, or given up.
	if rt.wakeFailed(st) {
		return nil
	}
	if ports := rt.portsFor(app); len(ports) > 0 {
		for _, port := range ports {
			if answers(port) {
				return ports
			}
		}
	}
	for _, c := range containers {
		_ = rt.Engine.Start(c)
	}
	deadline := time.Now().Add(wakeTimeout)
	for time.Now().Before(deadline) {
		rt.routesMu.Lock()
		rt.routeAt = time.Time{}
		rt.routesMu.Unlock()
		if ports := rt.portsFor(app); len(ports) > 0 {
			for _, port := range ports {
				if answers(port) {
					return ports
				}
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	failed = true
	return nil
}

var readyClient = &http.Client{
	Timeout:       2 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// answers reports whether the app itself responded. A TCP connect isn't enough: docker's port proxy accepts connections before the app is listening.
func answers(port int) bool {
	resp, err := readyClient.Get(fmt.Sprintf("http://127.0.0.1:%d/", port))
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	return true
}

func (rt *Router) portsFor(app string) []int {
	table, err := rt.routes()
	if err != nil {
		return nil
	}
	return table[app]
}
