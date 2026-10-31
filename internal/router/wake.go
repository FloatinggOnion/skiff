package router

import (
	"fmt"
	"net"
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
		rt.cachedRouteAt = time.Time{}
		rt.routesMu.Unlock()
		if ports := rt.portsFor(app); len(ports) > 0 {
			for _, port := range ports {
				if conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second); err == nil {
					conn.Close()
					return ports
				}
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	return nil
}

func (rt *Router) portsFor(app string) []int {
	routes, err := rt.routes()
	if err != nil {
		return nil
	}
	var ports []int
	for _, r := range routes {
		if r.App == app {
			ports = append(ports, r.HostPort)
		}
	}
	return ports
}
