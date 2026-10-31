package panel

import (
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/nuelScript/skiff/internal/registry"
	"github.com/nuelScript/skiff/internal/sleep"
)

var (
	sleepMu      sync.Mutex
	panelStarted = time.Now()
)

func sleepFile() string { return filepath.Join(skiffDir(), "sleeping.json") }

func isSleeping(app string) bool {
	_, ok := sleep.Load(sleepFile())[app]
	return ok
}

// lastActivity is when the app last had a human visitor, was deployed, or the panel started watching it — whichever is latest.
func lastActivity(app string) time.Time {
	last := panelStarted
	var ms int64
	if sqlDB.QueryRow(`SELECT COALESCE(MAX(t),0) FROM requests WHERE app=? AND bot=0`, app).Scan(&ms) == nil && ms > 0 {
		if t := time.UnixMilli(ms); t.After(last) {
			last = t
		}
	}
	var started int64
	if sqlDB.QueryRow(`SELECT COALESCE(MAX(started),0) FROM deploys WHERE app=?`, app).Scan(&started) == nil && started > 0 {
		if t := time.Unix(started, 0); t.After(last) {
			last = t
		}
	}
	return last
}

func shouldSleep(src Source, idleFor time.Duration) bool {
	return src.SleepAfter > 0 && !src.Autoscale && idleFor >= time.Duration(src.SleepAfter)*time.Minute
}

func appContainers(a registry.App) []string {
	if len(a.Replicas) == 0 {
		return []string{a.Container}
	}
	out := make([]string, 0, len(a.Replicas))
	for _, r := range a.Replicas {
		out = append(out, r.Container)
	}
	return out
}

func (p *Panel) syncSleep(now time.Time) {
	sleepMu.Lock()
	defer sleepMu.Unlock()
	apps, err := registry.Load()
	if err != nil {
		return
	}
	asleep := sleep.Load(sleepFile())
	changed := false

	// The router starts containers when a sleeping app is visited; once they run, it's awake again.
	for app := range asleep {
		a, ok := apps[app]
		if !ok {
			delete(asleep, app)
			changed = true
			continue
		}
		// Check what the registry runs now: a redeploy while asleep replaces the containers listed here.
		if p.anyRunning(appContainers(a)) {
			delete(asleep, app)
			changed = true
			p.refreshHostPorts(a)
			log.Printf("skiff: %s woke up", app)
		}
	}

	for name, a := range apps {
		if _, ok := asleep[name]; ok || a.Host != "" || isInflight(name) {
			continue
		}
		src, ok := getSource(name)
		if !ok || !shouldSleep(src, now.Sub(lastActivity(name))) {
			continue
		}
		containers := appContainers(a)
		if !p.anyRunning(containers) {
			continue
		}
		// Publish the sleep before stopping, so a request landing in between finds a way to wake it.
		asleep[name] = containers
		if err := sleep.Save(sleepFile(), asleep); err != nil {
			delete(asleep, name)
			continue
		}
		for _, c := range containers {
			_ = p.eng.Stop(c)
		}
		recordAudit(src.Team, AuditEntry{Actor: "skiff", Action: "project.sleep", Target: name,
			Detail: "no visitors for " + roundDur(time.Duration(src.SleepAfter)*time.Minute)})
		log.Printf("skiff: put %s to sleep", name)
	}
	if changed {
		_ = sleep.Save(sleepFile(), asleep)
	}
}

func (p *Panel) anyRunning(containers []string) bool {
	for _, c := range containers {
		if p.eng.State(c) == "running" {
			return true
		}
	}
	return false
}

// refreshHostPorts records the host ports a woken container came back on; docker picks new ones on start.
func (p *Panel) refreshHostPorts(a registry.App) {
	_ = registry.Update(func(apps map[string]registry.App) {
		cur, ok := apps[a.Name]
		if !ok {
			return
		}
		for i, r := range cur.Replicas {
			if port, err := p.eng.HostPort(r.Container, cur.Port); err == nil {
				cur.Replicas[i].HostPort = port
			}
		}
		if port, err := p.eng.HostPort(cur.Container, cur.Port); err == nil {
			cur.HostPort = port
		}
		apps[a.Name] = cur
	})
}

func (p *Panel) sleepLoop() {
	time.Sleep(2 * time.Minute)
	for {
		guard("syncSleep", func() { p.syncSleep(time.Now()) })
		time.Sleep(30 * time.Second)
	}
}
