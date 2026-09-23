package reconcile

import (
	"reflect"
	"testing"
	"time"

	"github.com/nuelScript/skiff/internal/docker"
	"github.com/nuelScript/skiff/internal/registry"
)

var (
	now    = time.Date(2026, 9, 15, 23, 55, 0, 0, time.UTC)
	cutoff = now.Add(-10 * time.Minute)
	old    = now.Add(-72 * time.Hour)
)

func ctr(name, app string, running bool, created time.Time) docker.ContainerInfo {
	port := 0
	if app != "" {
		port = 3000
	}
	return docker.ContainerInfo{Name: name, App: app, Port: port, Running: running, Created: created}
}

func names(cs []docker.ContainerInfo) []string {
	out := []string{}
	for _, c := range cs {
		out = append(out, c.Name)
	}
	return out
}

// The 2026-09-15 incident: the server registry pointed portfolio-v1 at a long-dead
// container while the live one had been deployed from a laptop, and hgm-cards was
// never registered at all. The old reaper deleted both live sites.
func TestPlanReapKeepsLiveSitesTheRegistryLostTrackOf(t *testing.T) {
	apps := map[string]registry.App{
		"portfolio-v1": {Name: "portfolio-v1", Container: "portfolio-v1-979542", Port: 3000},
		"www":          {Name: "www", Container: "www-8dea3d", Replicas: []registry.Replica{{Container: "www-8dea3d"}}},
	}
	cs := []docker.ContainerInfo{
		ctr("portfolio-v1-f9d05b", "portfolio-v1", true, old),
		ctr("hgm-cards-84ed26", "hgm-cards", true, old),
		ctr("www-8dea3d", "www", true, now.Add(-time.Minute)),
	}
	plan := PlanReap(apps, cs, cutoff)

	if len(plan.Remove) != 0 {
		t.Fatalf("removed live containers: %v", plan.Remove)
	}
	for app, want := range map[string][]string{
		"portfolio-v1": {"portfolio-v1-f9d05b"},
		"hgm-cards":    {"hgm-cards-84ed26"},
	} {
		if got := names(plan.Adopt[app]); !reflect.DeepEqual(got, want) {
			t.Errorf("adopt %s = %v, want %v", app, got, want)
		}
	}
}

func TestPlanReapStillCleansUpGarbage(t *testing.T) {
	apps := map[string]registry.App{
		"blog": {Name: "blog", Container: "blog-new", Replicas: []registry.Replica{{Container: "blog-new"}}},
	}
	cs := []docker.ContainerInfo{
		ctr("blog-new", "blog", true, old),
		ctr("blog-old", "blog", true, old),     // superseded: blog has a live registered replica
		ctr("blog-failed", "blog", false, old), // stopped leftover
		ctr("gone-app-1", "gone-app", false, old),
	}
	plan := PlanReap(apps, cs, cutoff)

	want := []string{"blog-failed", "blog-old", "gone-app-1"}
	if !reflect.DeepEqual(plan.Remove, want) {
		t.Errorf("remove = %v, want %v", plan.Remove, want)
	}
	if len(plan.Adopt) != 0 {
		t.Errorf("unexpected adoptions: %v", plan.Adopt)
	}
}

func TestPlanReapLeavesRecentAndUnlabelledContainersAlone(t *testing.T) {
	cs := []docker.ContainerInfo{
		ctr("mid-deploy-abc", "shop", true, now.Add(-2*time.Minute)),  // in-progress deploy
		ctr("mid-deploy-old", "shop", false, now.Add(-2*time.Minute)), // too recent even if stopped
		ctr("pg-main", "", true, old),                                 // running, no app label
	}
	plan := PlanReap(map[string]registry.App{}, cs, cutoff)
	if len(plan.Remove) != 0 || len(plan.Adopt) != 0 {
		t.Errorf("touched containers it should have left alone: %+v", plan)
	}
}

func TestPlanReapRepairsEntryWhoseContainersAreStopped(t *testing.T) {
	apps := map[string]registry.App{
		"api": {Name: "api", Container: "api-a", Replicas: []registry.Replica{{Container: "api-a"}}},
	}
	cs := []docker.ContainerInfo{
		ctr("api-a", "api", false, old),
		ctr("api-b", "api", true, old),
		ctr("api-c", "api", true, old),
	}
	plan := PlanReap(apps, cs, cutoff)
	if got := names(plan.Adopt["api"]); !reflect.DeepEqual(got, []string{"api-b", "api-c"}) {
		t.Errorf("adopt api = %v, want both running replicas", got)
	}
	if len(plan.Remove) != 0 {
		t.Errorf("removed = %v; the registered-but-stopped container isn't this plan's to remove", plan.Remove)
	}
}
