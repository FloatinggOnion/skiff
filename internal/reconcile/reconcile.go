// Package reconcile squares a Docker engine's skiff containers with the local registry.
//
// Both the panel's startup reaper and `skiff sync` used to delete every skiff
// container missing from the registry. That is only safe if the registry is
// complete, and it wasn't: a CLI deploy to a server over SSH recorded the app in
// the deployer's registry, never the server's, so the server's next panel
// restart deleted the live site. The policy here never deletes the only running
// instance of an app — it adopts it back into the registry instead.
package reconcile

import (
	"reflect"
	"sort"
	"time"

	"github.com/nuelScript/skiff/internal/docker"
	"github.com/nuelScript/skiff/internal/drain"
	"github.com/nuelScript/skiff/internal/registry"
)

// Plan is what reconciling should do. Containers in neither list are left alone.
type Plan struct {
	// Remove holds stopped leftovers and superseded versions of apps that still
	// have a running, registered replica.
	Remove []string
	// Adopt maps an app to running containers that should be (re)registered
	// rather than deleted: its registry entry is missing or points only at
	// containers that are gone or stopped.
	Adopt map[string][]docker.ContainerInfo
}

// PlanReap decides what to do with every container the registry doesn't reference.
// Only apps with Host == "" belong to this engine; containers created after
// cutoff are skipped so an in-progress deploy isn't touched.
func PlanReap(apps map[string]registry.App, cs []docker.ContainerInfo, cutoff time.Time) Plan {
	referenced := map[string]bool{}
	for _, a := range apps {
		for _, c := range containersOf(a) {
			referenced[c] = true
		}
	}
	running := map[string]bool{}
	for _, c := range cs {
		if c.Running {
			running[c.Name] = true
		}
	}
	// An app is live if at least one container its entry references is running.
	live := map[string]bool{}
	for name, a := range apps {
		for _, c := range containersOf(a) {
			if running[c] {
				live[name] = true
			}
		}
	}

	plan := Plan{Adopt: map[string][]docker.ContainerInfo{}}
	for _, c := range cs {
		if referenced[c.Name] {
			continue
		}
		if !c.Created.IsZero() && c.Created.After(cutoff) {
			continue
		}
		switch {
		case !c.Running:
			plan.Remove = append(plan.Remove, c.Name)
		case c.App != "" && live[c.App]:
			plan.Remove = append(plan.Remove, c.Name) // superseded version
		case c.App != "" && c.Port > 0:
			plan.Adopt[c.App] = append(plan.Adopt[c.App], c)
		}
		// Running with no app label: not ours to judge, leave it.
	}
	for app := range plan.Adopt {
		sort.Slice(plan.Adopt[app], func(i, j int) bool {
			return plan.Adopt[app][i].Name < plan.Adopt[app][j].Name
		})
	}
	sort.Strings(plan.Remove)
	return plan
}

// Result reports what Reap did.
type Result struct {
	Removed []string
	Adopted []string // app names re-registered from running containers
}

// Reap applies PlanReap to eng using the local registry.
func Reap(eng *docker.Engine, cutoff time.Time) (Result, error) {
	all, err := registry.Load()
	if err != nil {
		return Result{}, err
	}
	apps := map[string]registry.App{}
	for name, a := range all {
		if a.Host == "" {
			apps[name] = a
		}
	}
	plan := PlanReap(apps, withoutDraining(eng.SkiffContainers(), drain.Active(time.Now())), cutoff)

	var res Result
	for _, c := range plan.Remove {
		_ = eng.Stop(c)
		if eng.Remove(c) == nil {
			res.Removed = append(res.Removed, c)
		}
	}

	names := make([]string, 0, len(plan.Adopt))
	for app := range plan.Adopt {
		names = append(names, app)
	}
	sort.Strings(names)
	for _, app := range names {
		var reps []registry.Replica
		port := 0
		for _, c := range plan.Adopt[app] {
			hp, err := eng.HostPort(c.Name, c.Port)
			if err != nil {
				continue
			}
			reps = append(reps, registry.Replica{Container: c.Name, HostPort: hp})
			port = c.Port
		}
		if len(reps) == 0 {
			continue
		}
		planned, hadEntry := apps[app]
		adopted := false
		err := registry.Update(func(cur map[string]registry.App) {
			// Only adopt if nothing registered the app since we looked (e.g. a deploy).
			now, has := cur[app]
			if has != hadEntry || (has && !reflect.DeepEqual(now, planned)) {
				return
			}
			cur[app] = registry.App{
				Name:      app,
				Container: reps[0].Container,
				Port:      port,
				HostPort:  reps[0].HostPort,
				Replicas:  reps,
			}
			adopted = true
		})
		if err != nil {
			return res, err
		}
		if adopted {
			res.Adopted = append(res.Adopted, app)
		}
	}
	return res, nil
}

// withoutDraining leaves replaced containers to the router while they finish their open requests. One past its time counts again, in case the router isn't running to retire it.
func withoutDraining(cs []docker.ContainerInfo, draining map[string]bool) []docker.ContainerInfo {
	out := make([]docker.ContainerInfo, 0, len(cs))
	for _, c := range cs {
		if !draining[c.Name] {
			out = append(out, c)
		}
	}
	return out
}

func containersOf(a registry.App) []string {
	cs := []string{}
	if a.Container != "" {
		cs = append(cs, a.Container)
	}
	for _, rp := range a.Replicas {
		cs = append(cs, rp.Container)
	}
	return cs
}
