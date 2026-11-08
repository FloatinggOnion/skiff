package panel

import (
	"fmt"
	"log"
	"math"
	"runtime"
	"time"

	"github.com/nuelScript/skiff/internal/config"
	"github.com/nuelScript/skiff/internal/docker"
	"github.com/nuelScript/skiff/internal/health"
	"github.com/nuelScript/skiff/internal/registry"
)

const (
	autoscaleEvery     = 30 * time.Second
	autoscaleSettle    = 45 * time.Second // wait for the first resource samples
	autoscaleWindow    = 3                // minutes of CPU to average on
	autoscaleUpCool    = 60               // seconds between scale-ups (react fast)
	autoscaleDownCool  = 300              // seconds between scale-downs (retreat slowly)
	autoscaleHealthTTL = 30 * time.Second
)

// autoscaleLast holds the last scale time per app; the single control-loop writer means cooldowns need no lock.
var autoscaleLast = map[string]int64{}

func (s *resStore) recentCPU(app string, mins int) (float64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	buckets := s.apps[app]
	if buckets == nil {
		return 0, false
	}
	cutoff := time.Now().Unix() - int64(mins)*60
	var sum float64
	var n int
	for t, b := range buckets {
		if t >= cutoff && b.N > 0 {
			sum += b.CPUSum
			n += b.N
		}
	}
	if n == 0 {
		return 0, false
	}
	return sum / float64(n), true
}

func (p *Panel) autoscaleLoop() {
	time.Sleep(autoscaleSettle)
	tick := time.NewTicker(autoscaleEvery)
	defer tick.Stop()
	for range tick.C {
		guard("autoscaleLoop", p.autoscaleTick)
	}
}

func (p *Panel) autoscaleTick() {
	apps, err := registry.Load()
	if err != nil {
		return
	}
	for name, app := range apps {
		src, ok := getSource(name)
		if !ok || !src.Autoscale {
			continue
		}
		min, max, target := scaleBounds(src)
		cpu, ok := resStats.recentCPU(name, autoscaleWindow)
		if !ok {
			continue // no signal yet
		}
		current := len(app.Replicas)
		if current < 1 {
			current = 1
		}
		desired := desiredReplicas(cpu, target, min, max)
		if desired == current {
			continue
		}

		now := time.Now().Unix()
		if desired > current {
			if now-autoscaleLast[name] < autoscaleUpCool {
				continue
			}
			if !roomToScale(procMem()) {
				log.Printf("autoscale %s: staying at %d replicas, the server is nearly out of memory", name, current)
				continue
			}
			if added := p.scaleUp(app, src, desired-current); added > 0 {
				autoscaleLast[name] = now
				log.Printf("autoscale %s: %d→%d replicas (cpu %.0f%%, target %.0f%%)", name, current, current+added, cpu, target)
			}
		} else {
			if now-autoscaleLast[name] < autoscaleDownCool {
				continue
			}
			if removed := p.scaleDown(name, 1); removed > 0 {
				autoscaleLast[name] = now
				log.Printf("autoscale %s: %d→%d replicas (cpu %.0f%%, target %.0f%%)", name, current, current-removed, cpu, target)
			}
		}
	}
}

// desiredReplicas clamps ceil(cpu/target) to [min,max]; cpu is total app CPU across the current replicas, so this holds each replica near target%.
func desiredReplicas(cpu, target float64, min, max int) int {
	d := int(math.Ceil(cpu / target))
	if d < min {
		d = min
	}
	if d > max {
		d = max
	}
	return d
}

// maxReplicas grows with the server, so a bigger box isn't held to a small one's limit.
func maxReplicas() int {
	if n := 2 * runtime.NumCPU(); n > 10 {
		return n
	}
	return 10
}

// roomToScale refuses another replica when memory is nearly gone: adding one then pushes every app on the box toward the kernel's OOM killer.
func roomToScale(u resourceUsage) bool {
	return u.Total <= 0 || pct(u) < memWarnPct
}

func scaleBounds(src Source) (min, max int, target float64) {
	min, max = src.ScaleMin, src.ScaleMax
	if min < 1 {
		min = 1
	}
	if max < min {
		max = min
	}
	if limit := maxReplicas(); max > limit {
		max = limit
	}
	target = float64(src.ScaleCPU)
	if target < 1 {
		target = 70
	}
	return min, max, target
}

func (p *Panel) scaleUp(app registry.App, src Source, count int) int {
	image := fmt.Sprintf("skiff-%s:latest", app.Name)
	if !p.eng.ImageExists(image) {
		return 0
	}
	env, net := p.appRuntime(src)
	_ = p.eng.EnsureNetwork(net)
	mounts := p.appMounts(src)
	check := p.healthPath(src, app.Container)
	memory, cpu := p.replicaLimits(src, app.Container)

	added := 0
	for range count {
		container := fmt.Sprintf("%s-%s", app.Name, replicaSuffix())
		hostPort, err := p.eng.Run(docker.RunSpec{
			Name:          container,
			App:           app.Name,
			Image:         image,
			ContainerPort: app.Port,
			Settings: docker.Settings{
				Env: env, Network: net, Volumes: mounts, Memory: memory, CPU: cpu,
				Options: config.DecodeRunOptions(src.RunOptions),
			},
			Aliases: stackAliases(src),
			Command: stackCommand(src),
		})
		if err != nil {
			break
		}
		if health.WaitMode(src.HealthMode, "127.0.0.1", hostPort, check, autoscaleHealthTTL) != nil {
			_ = p.eng.Remove(container)
			break
		}
		// Register atomically so a concurrent deploy's write isn't clobbered (the app may have been torn down mid-scale).
		gone := false
		regErr := registry.Update(func(apps map[string]registry.App) {
			a, ok := apps[app.Name]
			if !ok {
				gone = true
				return
			}
			a.Replicas = append(a.Replicas, registry.Replica{Container: container, HostPort: hostPort})
			if a.Container == "" {
				a.Container, a.HostPort, a.Port = container, hostPort, app.Port
			}
			apps[app.Name] = a
		})
		if gone || regErr != nil {
			_ = p.eng.Remove(container)
			break
		}
		added++
	}
	return added
}

// scaleDown retires up to count replicas (never the last), re-reading under the registry lock so a deploy landing during the seconds-long drain isn't clobbered.
func (p *Panel) scaleDown(app string, count int) int {
	fresh, err := registry.Load()
	if err != nil {
		return 0
	}
	a, ok := fresh[app]
	if !ok {
		return 0
	}
	retire := map[string]bool{}
	reps := a.Replicas
	for i := 0; i < count && len(reps) > 1; i++ {
		last := reps[len(reps)-1]
		_ = p.eng.Stop(last.Container) // SIGTERM drains in-flight before removal
		_ = p.eng.Remove(last.Container)
		retire[last.Container] = true
		reps = reps[:len(reps)-1]
	}
	if len(retire) == 0 {
		return 0
	}
	_ = registry.Update(func(apps map[string]registry.App) {
		b, ok := apps[app]
		if !ok {
			return
		}
		kept := b.Replicas[:0]
		for _, rp := range b.Replicas {
			if !retire[rp.Container] {
				kept = append(kept, rp)
			}
		}
		if len(kept) == 0 {
			return
		}
		b.Replicas = kept
		b.Container, b.HostPort = kept[0].Container, kept[0].HostPort
		apps[app] = b
	})
	return len(retire)
}

// healthOf holds the health check a deploy uses: a preview follows its project's.
func healthOf(src Source) Source {
	if src.Parent != "" {
		if parent, ok := getSource(src.Parent); ok {
			return parent
		}
	}
	return src
}

// replicaLimits are a new replica's memory and CPU limits; a CLI app's come from its skiff.toml, recorded on its container.
func (p *Panel) replicaLimits(src Source, container string) (memory, cpu string) {
	if src.Origin == originCLI {
		if meta, err := p.eng.ContainerMeta(container); err == nil {
			return meta.Labels["skiff.memory"], meta.Labels["skiff.cpu"]
		}
	}
	hc := healthOf(src)
	return hc.Memory, hc.CPU
}

// healthPath is what a new replica must answer before it takes traffic; a CLI app's comes from its skiff.toml, recorded on its container.
func (p *Panel) healthPath(src Source, container string) string {
	if src.Origin == originCLI {
		if meta, err := p.eng.ContainerMeta(container); err == nil {
			return meta.Labels["skiff.health"]
		}
		return ""
	}
	return healthOf(src).HealthPath
}

func replicaSuffix() string { return randHex(3) }
