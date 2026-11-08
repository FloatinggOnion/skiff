package panel

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nuelScript/skiff/internal/config"
)

func TestVolumeDefs(t *testing.T) {
	openTestDB(t)
	p := &Panel{}
	for _, s := range []Source{
		{App: "shop", Repo: "acme/shop", Branch: "main", Port: "3000"},
		{App: "shop-feat-x", Repo: "acme/shop", Branch: "feat/x", Parent: "shop", Port: "3000"},
	} {
		if err := putSource(s); err != nil {
			t.Fatal(err)
		}
	}
	data := config.Volume{Name: "data", Path: "/data"}
	if err := addVolume("shop", data); err != nil {
		t.Fatal(err)
	}
	if addVolume("shop", config.Volume{Name: "data", Path: "/other"}) == nil {
		t.Fatal("a second volume with the same name was accepted")
	}

	shop, _ := getSource("shop")
	preview, _ := getSource("shop-feat-x")
	if got := p.volumeDefs(shop); len(got) != 1 || got[0] != data {
		t.Fatalf("project defs = %+v", got)
	}
	// A preview mounts the same paths, but on volumes named after itself, so it never touches production's files.
	if got := p.volumeDefs(preview); len(got) != 1 || got[0] != data {
		t.Fatalf("preview defs = %+v", got)
	}
	out := projectToml(preview, nil, teamNetwork(""), p.volumeDefs(preview))
	path := filepath.Join(t.TempDir(), "skiff.toml")
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("generated toml doesn't parse: %v\n%s", err, out)
	}
	if len(cfg.Volumes) != 1 || config.VolumeName(cfg.Name, cfg.Volumes[0].Name) != "skiff-shop-feat-x_data" {
		t.Fatalf("preview volumes = %+v", cfg.Volumes)
	}

	if !deleteVolume("shop", "data") || deleteVolume("shop", "data") {
		t.Fatal("deleteVolume should report whether it removed a row")
	}
	_ = addVolume("shop", data)
	deleteSource("shop")
	if len(listVolumes("shop")) != 0 {
		t.Fatal("deleting the project left its volumes behind")
	}
}

func TestNewVolume(t *testing.T) {
	openTestDB(t)
	if err := addVolume("shop", config.Volume{Name: "data", Path: "/data"}); err != nil {
		t.Fatal(err)
	}
	v, problem := newVolume("shop", config.Volume{Name: " uploads ", Path: " /uploads ", Owner: "someone-else"})
	if problem != "" || v.Name != "uploads" || v.Path != "/uploads" {
		t.Fatalf("newVolume = %+v, %q", v, problem)
	}
	if v.Owner != "" {
		t.Errorf("a volume added by hand belongs to %q, not its own app", v.Owner)
	}
	if _, problem := newVolume("shop", config.Volume{Name: "data", Path: "/elsewhere"}); problem == "" {
		t.Error("a second volume named data was accepted")
	}
}
