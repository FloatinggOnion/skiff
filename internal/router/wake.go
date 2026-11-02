package router

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nuelScript/skiff/internal/sleep"
)

const wakeTimeout = 30 * time.Second

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

// wake starts a sleeping app's containers and waits until one accepts connections, returning its ports; the request that woke it is held until then.
func (rt *Router) wake(app string) []int {
	containers := rt.sleepingApps()[app]
	if len(containers) == 0 {
		return nil
	}
	rt.wakeMu.Lock()
	defer rt.wakeMu.Unlock()
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
