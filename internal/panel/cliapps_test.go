package panel

import (
	"testing"

	"github.com/nuelScript/skiff/internal/registry"
)

func TestAdoptCLIApps(t *testing.T) {
	openTestDB(t)
	putSource(Source{App: "git-app", Team: "other", Port: "3000"})
	apps := map[string]registry.App{
		"git-app":  {Name: "git-app", Port: 3000},
		"cli-app":  {Name: "cli-app", Port: 8080, Replicas: []registry.Replica{{Container: "a"}, {Container: "b"}}},
		"far-away": {Name: "far-away", Port: 80, Host: "root@elsewhere"},
	}

	got := adoptCLIApps(apps, "t1")
	if len(got) != 1 || got[0] != "cli-app" {
		t.Fatalf("adopted %v, want [cli-app]", got)
	}
	src, ok := getSource("cli-app")
	if !ok || src.Team != "t1" || src.Origin != originCLI || src.Port != "8080" || src.Replicas != 2 {
		t.Fatalf("adopted source = %+v", src)
	}
	if g, _ := getSource("git-app"); g.Team != "other" || g.Origin != "" {
		t.Fatalf("existing project was touched: %+v", g)
	}
	if _, ok := getSource("far-away"); ok {
		t.Fatal("adopted an app that runs on another host")
	}
	if again := adoptCLIApps(apps, "t1"); len(again) != 0 {
		t.Fatalf("second pass adopted %v", again)
	}
	if none := adoptCLIApps(map[string]registry.App{"x": {Name: "x"}}, ""); none != nil {
		t.Fatal("adopted without a team to put it in")
	}
}

func TestSourceOriginSurvivesUpsert(t *testing.T) {
	openTestDB(t)
	putSource(Source{App: "c", Team: "t1", Port: "80", Origin: originCLI})
	src, _ := getSource("c")
	src.Replicas = 3
	putSource(src)
	if got, _ := getSource("c"); got.Origin != originCLI || got.Replicas != 3 {
		t.Fatalf("got %+v", got)
	}
}
