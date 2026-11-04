package panel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/nuelScript/skiff/internal/compose"
	"github.com/nuelScript/skiff/internal/config"
)

func TestTomlQuoteRoundTrips(t *testing.T) {
	for _, s := range []string{
		"plain", `a"b`, `a\b`, "line\nbreak", "tab\t", "\x01\x07\x0b\x1f\x7f", "\"\n[build]\nimage = \"evil\"", "caf\u00e9 \U0001F600",
		"x\"\n\n[secrets]\nA=\"b",
	} {
		var out struct{ V string }
		if _, err := toml.Decode("V = "+tomlQuote(s), &out); err != nil {
			t.Errorf("%q: %v", s, err)
			continue
		}
		if out.V != s {
			t.Errorf("%q came back as %q", s, out.V)
		}
	}
}

func TestProjectTomlTreatsPortAsNumber(t *testing.T) {
	out := projectToml(Source{App: "web", Port: "3000\n[deploy]\nrelease = \"x\""}, nil, "net", nil)
	if strings.Contains(out, "release") || strings.Contains(out, "port") {
		t.Fatalf("an invalid port must not be written:\n%s", out)
	}
	out = projectToml(Source{App: "web", Port: "8080", Release: "a\"\n[build]\nimage=\"evil"}, nil, "net", nil)
	path := filepath.Join(t.TempDir(), "skiff.toml")
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if cfg.Build.Port != 8080 || cfg.Build.Image != "" || !strings.Contains(cfg.Deploy.Release, "[build]") {
		t.Fatalf("release value escaped its string: %+v", cfg)
	}
}

func TestStackLimits(t *testing.T) {
	s := &stackSource{cloneURL: "https://github.com/a/b", dir: ".", plan: &compose.Plan{Stack: "s", Items: []compose.Item{
		{Service: "a", Name: "s-a", Kind: compose.KindApp, Image: "x:1", Replicas: 100000},
		{Service: "b", Name: "s-b", Kind: compose.KindApp, Replicas: 1, Build: &compose.Build{Context: ".", Dockerfile: "../../etc/Dockerfile"}},
		{Service: "c", Name: "s-c", Kind: compose.KindApp, Replicas: 1, Build: &compose.Build{Context: ".", Dockerfile: "docker/Dockerfile"}},
	}}}
	openTestDB(t)
	checkStack(s, "t1", false)
	if got := s.plan.Items[0].Replicas; got != maxReplicas() {
		t.Errorf("replicas = %d, want the cap %d", got, maxReplicas())
	}
	var bad, c int
	for _, p := range s.plan.Problems {
		if strings.HasPrefix(p, "b:") && strings.Contains(p, "dockerfile") {
			bad++
		}
		if strings.HasPrefix(p, "c:") && strings.Contains(p, "dockerfile") {
			c++
		}
	}
	if bad != 1 || c != 0 {
		t.Errorf("dockerfile problems: b=%d c=%d, in %v", bad, c, s.plan.Problems)
	}
}
