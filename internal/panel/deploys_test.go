package panel

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nuelScript/skiff/internal/config"
)

func TestProjectToml(t *testing.T) {
	src := Source{App: "api", Team: "t1", Port: "3000", Replicas: 2, Release: "npm run migrate"}
	env := []EnvVar{
		{Key: "API_URL", Value: "https://x", Build: true}, // build + runtime
		{Key: "SECRET_TOKEN", Value: "shh", Build: false}, // runtime-only secret
	}
	out := projectToml(src, env, teamNetwork(src.Team), nil)

	path := filepath.Join(t.TempDir(), "skiff.toml")
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("generated toml doesn't parse: %v\n%s", err, out)
	}

	if cfg.Name != "api" || cfg.Replicas != 2 || cfg.Build.Port != 3000 {
		t.Fatalf("basics wrong: %+v", cfg)
	}
	if cfg.Deploy.Network != teamNetwork("t1") {
		t.Fatalf("network = %q, want %q", cfg.Deploy.Network, teamNetwork("t1"))
	}
	if cfg.Deploy.Release != "npm run migrate" {
		t.Fatalf("release = %q", cfg.Deploy.Release)
	}
	// The secret must NOT leak into [env], where it would bake into the image; the build var belongs there.
	if cfg.Env["API_URL"] != "https://x" {
		t.Errorf("build var missing from [env]: %v", cfg.Env)
	}
	if _, leaked := cfg.Env["SECRET_TOKEN"]; leaked {
		t.Errorf("secret leaked into [env]: %v", cfg.Env)
	}
	if cfg.Secrets["SECRET_TOKEN"] != "shh" {
		t.Errorf("secret missing from [secrets]: %v", cfg.Secrets)
	}

	bare := projectToml(Source{App: "web", Port: "8080"}, nil, teamNetwork(""), nil)
	if strings.Contains(bare, "replicas") || strings.Contains(bare, "release") {
		t.Errorf("bare config should omit replicas/release:\n%s", bare)
	}
}

// A Compose service can restart for a while until a dependency finishes its own setup, so it gets longer to come up unless someone chose otherwise.
func TestStackServicesGetLongerToStart(t *testing.T) {
	timeout := func(src Source) int {
		path := filepath.Join(t.TempDir(), "skiff.toml")
		if err := os.WriteFile(path, []byte(projectToml(src, nil, teamNetwork(""), nil)), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		return cfg.Deploy.HealthTimeout
	}
	for _, c := range []struct {
		name string
		src  Source
		want int
	}{
		{"an ordinary project keeps the default", Source{App: "web", Port: "8080"}, 0},
		{"a stack service gets longer", Source{App: "c-apisix", Port: "9080", Stack: "c"}, stackHealthTimeout},
		{"a chosen timeout wins", Source{App: "c-apisix", Port: "9080", Stack: "c", HealthTimeout: 45}, 45},
		{"a chosen timeout wins outside a stack too", Source{App: "web", Port: "8080", HealthTimeout: 45}, 45},
	} {
		if got := timeout(c.src); got != c.want {
			t.Errorf("%s: health_timeout = %d, want %d", c.name, got, c.want)
		}
		if got := defaultHealthTimeout(c.src); c.src.Stack != "" && got != stackHealthTimeout || c.src.Stack == "" && got != 30 {
			t.Errorf("%s: defaultHealthTimeout = %d", c.name, got)
		}
	}
}

func TestInjectToken(t *testing.T) {
	if got := injectToken("https://github.com/acme/api.git", "tok"); got != "https://tok@github.com/acme/api.git" {
		t.Fatalf("https inject = %q", got)
	}
	// Non-https URLs pass through untouched — the token must never be spliced into
	// an unexpected scheme.
	for _, u := range []string{"http://x/y", "git@github.com:acme/api.git", "ssh://git@host/repo"} {
		if got := injectToken(u, "tok"); got != u {
			t.Fatalf("injectToken(%q) modified a non-https URL: %q", u, got)
		}
	}
}

func TestBeginBuildSupersede(t *testing.T) {
	_, superseded1, done1 := beginBuild("supersede-app", "id1")
	defer done1()
	if superseded1() {
		t.Fatal("first build reported superseded before any newer build")
	}

	_, superseded2, done2 := beginBuild("supersede-app", "id2")
	defer done2()
	if !superseded1() {
		t.Fatal("first build not superseded by a newer one")
	}
	if superseded2() {
		t.Fatal("second (current) build wrongly reported superseded")
	}

	done1()
	if superseded2() {
		t.Fatal("cleanup of the first build evicted the current one")
	}
}

func TestCancelInflight(t *testing.T) {
	_, superseded, done := beginBuild("cancel-app", "idA")
	defer done()

	if cancelInflight("cancel-app", "idB") {
		t.Fatal("cancelInflight matched a non-current id")
	}
	if !cancelInflight("cancel-app", "idA") {
		t.Fatal("cancelInflight didn't match the current id")
	}
	if !superseded() {
		t.Fatal("a canceled build should read as superseded (ctx canceled)")
	}
}

func TestProjectTomlHealth(t *testing.T) {
	openTestDB(t)
	parent := Source{App: "api", Port: "3000", HealthPath: "/healthz", HealthTimeout: 90}
	preview := Source{App: "api-feat", Port: "3000", Parent: "api"}
	for _, s := range []Source{parent, preview} {
		if err := putSource(s); err != nil {
			t.Fatal(err)
		}
	}
	stored, _ := getSource("api")
	if stored.HealthPath != "/healthz" || stored.HealthTimeout != 90 {
		t.Fatalf("health settings didn't round-trip: %+v", stored)
	}
	for _, src := range []Source{parent, preview} {
		path := filepath.Join(t.TempDir(), "skiff.toml")
		if err := os.WriteFile(path, []byte(projectToml(src, nil, teamNetwork(""), nil)), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Deploy.HealthPath != "/healthz" || cfg.Deploy.HealthTimeout != 90 {
			t.Errorf("%s: deploy = %+v, want the project's health check", src.App, cfg.Deploy)
		}
	}
	if strings.Contains(projectToml(Source{App: "web", Port: "8080"}, nil, teamNetwork(""), nil), "health") {
		t.Error("a project without a health check shouldn't set one")
	}
}

func TestDeployConfigIsPrivateAndTemporary(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_ = os.MkdirAll(skiffDir(), 0o700)
	path, clean, err := writeDeployConfig("secret")
	if err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(path); info == nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info)
	}
	clean()
	if _, err := os.Stat(filepath.Dir(path)); err == nil {
		t.Fatal("the config should be gone after the deploy")
	}
}

func TestRemoveStaleDeployConfigs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	builds := filepath.Join(skiffDir(), "builds")
	old := filepath.Join(builds, "shop", "skiff.toml")
	nested := filepath.Join(builds, "mono", "apps", "web", "skiff.toml")
	fresh := filepath.Join(builds, "busy", "skiff.toml")
	other := filepath.Join(builds, "shop", "package.json")
	for _, f := range []string{old, nested, fresh, other} {
		_ = os.MkdirAll(filepath.Dir(f), 0o755)
		_ = os.WriteFile(f, []byte("x"), 0o600)
	}
	long := time.Now().Add(-3 * deployDeadline)
	_ = os.Chtimes(old, long, long)
	_ = os.Chtimes(nested, long, long)
	removeStaleDeployConfigs(builds, time.Now())
	for f, want := range map[string]bool{old: false, nested: false, fresh: true, other: true} {
		if _, err := os.Stat(f); (err == nil) != want {
			t.Errorf("%s exists = %v, want %v", f, err == nil, want)
		}
	}
}

func TestTailLogAlwaysEnds(t *testing.T) {
	openTestDB(t)
	t.Setenv("HOME", t.TempDir())
	was := tailNoDeployAfter
	tailNoDeployAfter = 100 * time.Millisecond
	t.Cleanup(func() { tailNoDeployAfter = was })
	p := &Panel{}
	for status, want := range map[string]string{
		"":         "[done] error",
		"failed":   "[done] error",
		"canceled": "[done] error",
		"live":     "[done] ok",
	} {
		id := "id-" + status
		if status != "" {
			addDeploy(Deploy{ID: id, App: "a", Status: status, Started: 1})
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		rec := httptest.NewRecorder()
		p.tailLog(rec, httptest.NewRequest("GET", "/", nil).WithContext(ctx), "a", id)
		timedOut := ctx.Err() != nil
		cancel()
		if timedOut || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("status %q: timed out=%v, body %q", status, timedOut, rec.Body.String())
		}
	}
}
