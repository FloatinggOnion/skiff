package router

import (
	"log"
	"time"

	"github.com/nuelScript/skiff/internal/drain"
)

const (
	drainTendEvery = 2 * time.Second
	// drainMinimum outlasts any route table that still lists the container, so requests sent from one find it running.
	drainMinimum = routesTooOld
	// drainStopWithin is how long an app gets to exit after being asked, once its open requests are done or out of time.
	drainStopWithin = 30
)

// trackOpen counts a request or connection to a port until the returned func is called.
func (rt *Router) trackOpen(port int) func() {
	rt.openMu.Lock()
	if rt.open == nil {
		rt.open = map[int]int{}
	}
	rt.open[port]++
	rt.openMu.Unlock()
	return func() {
		rt.openMu.Lock()
		defer rt.openMu.Unlock()
		if rt.open[port]--; rt.open[port] <= 0 {
			delete(rt.open, port)
		}
	}
}

func (rt *Router) openOn(port int) int {
	rt.openMu.Lock()
	defer rt.openMu.Unlock()
	return rt.open[port]
}

func (rt *Router) startDrain() {
	go func() {
		for {
			drain.Beat()
			time.Sleep(drain.RouterBeatEvery)
		}
	}()
	go func() {
		retiring := map[string]bool{}
		done := make(chan string, 16)
		for {
			for len(done) > 0 {
				delete(retiring, <-done)
			}
			now := time.Now()
			for _, e := range drain.Load() {
				if !e.Starting && !retiring[e.Container] && drainFinished(e, rt.openOn(e.HostPort), now) {
					retiring[e.Container] = true
					go func() {
						rt.retire(e)
						done <- e.Container
					}()
				}
			}
			time.Sleep(drainTendEvery)
		}
	}()
}

func drainFinished(e drain.Entry, open int, now time.Time) bool {
	if now.Unix() >= e.Until {
		return true
	}
	return open == 0 && now.Unix() >= e.Since+int64(drainMinimum/time.Second)
}

func (rt *Router) retire(e drain.Entry) {
	_ = rt.Engine.StopWithin(e.Container, drainStopWithin)
	if err := rt.Engine.Remove(e.Container); err != nil {
		log.Printf("router: removing replaced container %s: %v", e.Container, err)
	}
	if err := drain.Remove(e.Container); err != nil {
		log.Printf("router: forgetting replaced container %s: %v", e.Container, err)
	}
}
