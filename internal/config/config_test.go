package config

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestEnvironmentMerge(t *testing.T) {
	dir := t.TempDir()
	dotenv := "# comment\n\nexport FOO=from_dotenv\nBAR=\"quoted\"\nBAZ='single'\nOVERRIDE=dotenv\nnoequals\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(dotenv), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Config{Env: map[string]string{"OVERRIDE": "config", "NEW": "x"}}
	env := c.Environment(dir)

	want := map[string]string{
		"FOO":      "from_dotenv",
		"BAR":      "quoted",
		"BAZ":      "single",
		"OVERRIDE": "config",
		"NEW":      "x",
	}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("env[%q] = %q, want %q", k, env[k], v)
		}
	}
	if _, ok := env["noequals"]; ok {
		t.Error("a line without '=' should be skipped")
	}
}

func TestEnvironmentNoDotenv(t *testing.T) {
	c := &Config{Env: map[string]string{"A": "1"}}
	env := c.Environment(t.TempDir())
	if len(env) != 1 || env["A"] != "1" {
		t.Errorf("missing .env should yield just the config env, got %v", env)
	}
}

func TestLoadDefaults(t *testing.T) {
	p := write(t, "skiff.toml", "name = \"api\"\n")
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Name != "api" {
		t.Errorf("name = %q", c.Name)
	}
	if c.Build.Port != 8080 {
		t.Errorf("default port = %d, want 8080", c.Build.Port)
	}
	if c.Replicas != 1 {
		t.Errorf("default replicas = %d, want 1", c.Replicas)
	}
	if c.Build.Dockerfile != "Dockerfile" {
		t.Errorf("default dockerfile = %q", c.Build.Dockerfile)
	}
}

func TestLoadRequiresName(t *testing.T) {
	p := write(t, "skiff.toml", "replicas = 2\n")
	if _, err := Load(p); err == nil {
		t.Fatal("Load accepted a config with no name")
	}
}

func TestLoadValues(t *testing.T) {
	p := write(t, "skiff.toml", `
name = "web"
replicas = 4
[build]
port = 3000
[deploy]
release = "npm run migrate"
`)
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Build.Port != 3000 || c.Replicas != 4 || c.Deploy.Release != "npm run migrate" {
		t.Fatalf("parsed wrong: %+v", c)
	}
}

func TestLoadVolumes(t *testing.T) {
	c, err := Load(write(t, "skiff.toml", "name = \"app\"\n\n[[volumes]]\nname = \"data\"\npath = \"/data\"\n\n[[volumes]]\nname = \"uploads\"\npath = \"/app/public/uploads\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Volumes) != 2 || c.Volumes[1] != (Volume{Name: "uploads", Path: "/app/public/uploads"}) {
		t.Fatalf("volumes = %+v", c.Volumes)
	}
	if got := VolumeName("app", "data"); got != "skiff-app_data" {
		t.Errorf("VolumeName = %q", got)
	}
}

func TestValidateVolumes(t *testing.T) {
	bad := [][]Volume{
		{{Name: "", Path: "/data"}},
		{{Name: "Data", Path: "/data"}},
		{{Name: "-x", Path: "/data"}},
		{{Name: "a_b", Path: "/data"}},
		{{Name: "data", Path: "data"}},
		{{Name: "data", Path: "/"}},
		{{Name: "data", Path: "/data/../etc"}},
		{{Name: "data", Path: "/data/"}},
		{{Name: "data", Path: "/data:ro"}},
		{{Name: "data", Path: "/proc/x"}},
		{{Name: "data", Path: "/dev"}},
		{{Name: "a", Path: "/a"}, {Name: "a", Path: "/b"}},
		{{Name: "a", Path: "/a"}, {Name: "b", Path: "/a"}},
		{{Name: "a", Path: "/a"}, {Name: "b", Path: "/b"}, {Name: "c", Path: "/c"}, {Name: "d", Path: "/d"}, {Name: "e", Path: "/e"}, {Name: "f", Path: "/f"}},
	}
	for _, v := range bad {
		if ValidateVolumes(v) == nil {
			t.Errorf("accepted %+v", v)
		}
	}
	if err := ValidateVolumes([]Volume{{Name: "data", Path: "/data"}, {Name: "cache-2", Path: "/var/cache/app"}}); err != nil {
		t.Errorf("rejected a valid set: %v", err)
	}
}

func TestLoadHealth(t *testing.T) {
	c, err := Load(write(t, "skiff.toml", "name = \"app\"\n\n[deploy]\nhealth_path = \"/healthz?full=1\"\nhealth_timeout = 90\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Deploy.HealthPath != "/healthz?full=1" || c.Deploy.HealthTimeout != 90 {
		t.Fatalf("deploy = %+v", c.Deploy)
	}
	for _, tc := range []struct {
		path    string
		timeout int
	}{{"healthz", 0}, {"/a b", 0}, {"/a\tb", 0}, {"/ok", 4}, {"/ok", 601}, {"/ok", -1}} {
		if ValidateHealth(tc.path, tc.timeout) == nil {
			t.Errorf("accepted %q %d", tc.path, tc.timeout)
		}
	}
	if ValidateHealth("", 0) != nil || ValidateHealth("/up", 5) != nil {
		t.Error("rejected a valid health check")
	}
}

func TestImageConfig(t *testing.T) {
	p := write(t, "skiff.toml", "name = \"web\"\n[build]\nimage = \"nginx:1.27\"\n")
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Build.Image != "nginx:1.27" || c.Build.Port != 0 {
		t.Errorf("image deploy should leave the port to the image, got image=%q port=%d", c.Build.Image, c.Build.Port)
	}
	if _, err := Load(write(t, "skiff.toml", "name = \"web\"\n[build]\nimage = \"nginx latest\"\n")); err == nil {
		t.Error("an image reference with a space was accepted")
	}
}

func TestValidateImage(t *testing.T) {
	for _, ok := range []string{"nginx", "nginx:1.27", "ghcr.io/you/api:sha-1a2b", "localhost:5000/app", "you/app@sha256:abc123"} {
		if err := ValidateImage(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "-flag", "a b", "app;rm", "app\n"} {
		if ValidateImage(bad) == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestRegistryHost(t *testing.T) {
	for ref, want := range map[string]string{
		"nginx":                    "docker.io",
		"you/app:1":                "docker.io",
		"ghcr.io/you/app":          "ghcr.io",
		"localhost:5000/app":       "localhost:5000",
		"registry.example.com/a/b": "registry.example.com",
		"localhost/app":            "localhost",
	} {
		if got := RegistryHost(ref); got != want {
			t.Errorf("RegistryHost(%q) = %q, want %q", ref, got, want)
		}
	}
}

func TestValidateResources(t *testing.T) {
	for _, ok := range [][2]string{{"", ""}, {"512m", ""}, {"2g", "0.5"}, {"1.5g", "2"}, {"", "1"}} {
		if err := ValidateResources(ok[0], ok[1]); err != nil {
			t.Errorf("%v: %v", ok, err)
		}
	}
	for _, bad := range [][2]string{{"512", ""}, {"m", ""}, {"512mb", ""}, {"5m", ""}, {"0g", ""}, {"-1g", ""}, {"", "x"}, {"", "0"}, {"", "-1"}, {"1g --privileged", ""}} {
		if ValidateResources(bad[0], bad[1]) == nil {
			t.Errorf("%v was accepted", bad)
		}
	}
}
