package docker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nuelScript/skiff/internal/config"
)

func TestParsePercent(t *testing.T) {
	cases := map[string]float64{
		"0.00%": 0, "12.34%": 12.34, "100.09%": 100.09, "  5.5% ": 5.5, "--": 0, "": 0,
	}
	for in, want := range cases {
		if got := parsePercent(in); got != want {
			t.Errorf("parsePercent(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParseSize(t *testing.T) {
	unit := func(v float64, shift uint) int64 { return int64(v * float64(int64(1)<<shift)) }
	cases := []struct {
		in   string
		want int64
	}{
		{"0B", 0},
		{"512B", 512},
		{"15.87MiB", unit(15.87, 20)},
		{"7.63GiB", unit(7.63, 30)},
		{"1.5KiB", unit(1.5, 10)},
		{"2GB", 2 << 30},
		{"", 0},
	}
	for _, c := range cases {
		if got := parseSize(c.in); got != c.want {
			t.Errorf("parseSize(%q) = %d, want %d", c.in, got, c.want)
		}
	}
	// Trailing space (from the "used / limit" split) must be trimmed.
	if got := parseSize("15.87MiB "); got != unit(15.87, 20) {
		t.Errorf("trailing space not trimmed: %d", got)
	}
}

func TestParseSkiffContainers(t *testing.T) {
	out := []byte("portfolio-v1-f9d05b|2026-07-05 10:56:01 +0000 UTC|portfolio-v1|3000|running\n" +
		"pg-main|2026-07-04 22:30:00 +0000 UTC|||exited\n" +
		"\n")
	cs := parseSkiffContainers(out)
	if len(cs) != 2 {
		t.Fatalf("got %d containers, want 2: %+v", len(cs), cs)
	}
	app := cs[0]
	if app.Name != "portfolio-v1-f9d05b" || app.App != "portfolio-v1" || app.Port != 3000 || !app.Running {
		t.Errorf("app container parsed wrong: %+v", app)
	}
	if app.Created.IsZero() {
		t.Errorf("created time not parsed")
	}
	if db := cs[1]; db.App != "" || db.Port != 0 || db.Running {
		t.Errorf("unlabelled stopped container parsed wrong: %+v", db)
	}
}

func TestParseCreatedAt(t *testing.T) {
	got := parseCreatedAt("2026-09-23 19:54:00 +0000 UTC")
	if got.Year() != 2026 || got.Month() != 9 || got.Hour() != 19 || got.Minute() != 54 {
		t.Fatalf("parsed %v", got)
	}
}

func TestParseVolumeSizes(t *testing.T) {
	out := []byte(`[{"Name":"skiff-app_data","Size":"64.63MB"},{"Name":"skiff-npm","Size":"0B"}]` + "\n")
	got := parseVolumeSizes(out)
	want := 64.63 * (1 << 20)
	if got["skiff-app_data"] != int64(want) || got["skiff-npm"] != 0 || len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	if len(parseVolumeSizes([]byte("null"))) != 0 {
		t.Fatal("null output should give no sizes")
	}
}

func TestParseExposedPort(t *testing.T) {
	for in, want := range map[string]int{
		`{"80/tcp":{}}`:                80,
		`{"8080/tcp":{},"443/tcp":{}}`: 443,
		`{"53/udp":{},"9000/tcp":{}}`:  9000,
		`{"53/udp":{}}`:                0,
		`null`:                         0,
		``:                             0,
	} {
		if got := parseExposedPort([]byte(in)); got != want {
			t.Errorf("parseExposedPort(%s) = %d, want %d", in, got, want)
		}
	}
}

func TestDockerAuthConfig(t *testing.T) {
	got := string(dockerAuthConfig(RegistryAuth{Host: "ghcr.io", Username: "u", Password: "p"}))
	if got != `{"auths":{"ghcr.io":{"auth":"dTpw"}}}` {
		t.Errorf("ghcr config = %s", got)
	}
	if hub := string(dockerAuthConfig(RegistryAuth{Host: "docker.io", Username: "u", Password: "p"})); !strings.Contains(hub, "https://index.docker.io/v1/") {
		t.Errorf("docker hub config = %s", hub)
	}
}

func TestSafetyCap(t *testing.T) {
	if got := safetyCap(4 << 30); got < 3200000000 || got > 3221225472 {
		t.Fatalf("cap on 4 GB = %d", got)
	}
}

func TestDockerignoreKeepsConfigOut(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".dockerignore")
	clean := ensureDockerignore(dir)
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "skiff.toml") {
		t.Fatalf("default file = %q", b)
	}
	clean()
	if _, err := os.Stat(path); err == nil {
		t.Fatal("the default file should be removed afterwards")
	}

	const own = "dist\n!keep\n!.git"
	_ = os.WriteFile(path, []byte(own), 0o644)
	clean = ensureDockerignore(dir)
	b, _ = os.ReadFile(path)
	if !strings.HasPrefix(string(b), own) || !strings.HasSuffix(string(b), "\n.git\nskiff.toml\n!.skiff-mounts\n!.skiff-mounts/**\n") {
		t.Fatalf("during the build = %q", b)
	}
	clean()
	if b, _ = os.ReadFile(path); string(b) != own {
		t.Fatalf("the app's own file should come back unchanged: %q", b)
	}
}

func TestSortDeployTagsNewestFirst(t *testing.T) {
	tags := []string{"1793773891-a", "1793784138-b", "1793773895-c", "custom", "1793784154-d"}
	sortDeployTags(tags)
	want := []string{"1793784154-d", "1793784138-b", "1793773895-c", "1793773891-a", "custom"}
	if strings.Join(tags, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v", tags)
	}
}

func TestAppContainersAreLimitedAndLocal(t *testing.T) {
	args := strings.Join((&Engine{}).runArgs(RunSpec{Name: "a-1", App: "a", Image: "img", ContainerPort: 3000, Settings: Settings{Memory: "256m"}}), " ")
	for _, want := range []string{
		"--pids-limit 4096", "-p 127.0.0.1::3000",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("missing %q in: %s", want, args)
		}
	}
	if strings.Contains(args, "0.0.0.0") {
		t.Errorf("an app must never be published on every interface: %s", args)
	}
	if got := strings.Join((&Engine{}).limitArgs(), " "); !strings.Contains(got, "--pids-limit 4096") {
		t.Errorf("limitArgs = %s", got)
	}
}

func TestRunBindsLoopbackUnlessPublic(t *testing.T) {
	e := &Engine{}
	if got := strings.Join(e.runArgs(RunSpec{Name: "a", Image: "i", ContainerPort: 3000}), " "); !strings.Contains(got, "-p 127.0.0.1::3000") {
		t.Errorf("default bind: %s", got)
	}
	if got := strings.Join(e.runArgs(RunSpec{Name: "a", Image: "i", ContainerPort: 3000, Public: true}), " "); !strings.Contains(got, "-p 0.0.0.0::3000") {
		t.Errorf("public bind: %s", got)
	}
}

func TestSettingsMeanTheSameEverywhere(t *testing.T) {
	e := &Engine{}
	s := Settings{
		Memory: "256m", CPU: "0.5", Network: "net", Env: map[string]string{"A": "1"},
		Volumes: []Mount{{Name: "vol", Destination: "/data"}},
		Options: config.RunOptions{User: "app", WorkingDir: "/srv"},
	}
	shared := strings.Join(e.settingsArgs(s), " ")
	for _, want := range []string{"--pids-limit 4096", "--memory 256m", "--cpus 0.5", "--network net", "-e A=1", "--user app", "--workdir /srv", "/data"} {
		if !strings.Contains(shared, want) {
			t.Errorf("missing %q in: %s", want, shared)
		}
	}
	if app := strings.Join(e.runArgs(RunSpec{Settings: s, Name: "a", App: "a", Image: "i", ContainerPort: 3000}), " "); !strings.Contains(app, shared) {
		t.Errorf("an app's container doesn't get the shared settings:\n%s\nwant %s", app, shared)
	}
}

func TestPullHint(t *testing.T) {
	for _, tc := range []struct {
		out   string
		creds bool
		want  string
	}{
		{"no matching manifest for linux/amd64 in the manifest list entries", false, "no build for the server's platform"},
		{"Error response from daemon: Head \"https://localhost:5000/v2/x/manifests/1\": no basic auth credentials", false, "sign in to its registry"},
		{"unauthorized: authentication required", true, "refused the username and password"},
		{"Error response from daemon: manifest for x:9 not found: manifest unknown", false, "no image with that name and tag"},
		{"dial tcp: lookup registry.example.com: no such host", false, ""},
	} {
		if got := PullHint(tc.out, tc.creds); tc.want == "" && got != "" || !strings.Contains(got, tc.want) {
			t.Errorf("%q: %q, want it to mention %q", tc.out, got, tc.want)
		}
	}
}
