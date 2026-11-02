package panel

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nuelScript/skiff/internal/config"
)

func TestImageProjectToml(t *testing.T) {
	for _, port := range []string{"", "9000"} {
		src := Source{App: "kuma", Image: "louislam/uptime-kuma:1", Port: port}
		out := projectToml(src, nil, teamNetwork(""), nil)
		path := filepath.Join(t.TempDir(), "skiff.toml")
		if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load(path)
		if err != nil {
			t.Fatalf("generated toml doesn't parse: %v\n%s", err, out)
		}
		if cfg.Build.Image != src.Image {
			t.Fatalf("image = %q", cfg.Build.Image)
		}
		// No port means the deploy reads it from the image.
		want := 0
		if port != "" {
			want = 9000
		}
		if cfg.Build.Port != want {
			t.Fatalf("port %q rendered as %d", port, cfg.Build.Port)
		}
	}
}

func TestRegistryCreds(t *testing.T) {
	openTestDB(t)
	if err := putSource(Source{App: "api", Image: "ghcr.io/acme/api:1"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := getSource("api"); got.Image != "ghcr.io/acme/api:1" {
		t.Fatalf("image didn't round-trip: %q", got.Image)
	}
	if _, _, ok := getRegistryCreds("api"); ok {
		t.Fatal("credentials before any were set")
	}
	_ = setRegistryCreds("api", "bot", "one")
	_ = setRegistryCreds("api", "bot", "two")
	if u, pw, ok := getRegistryCreds("api"); !ok || u != "bot" || pw != "two" {
		t.Fatalf("creds = %q %q %v", u, pw, ok)
	}
	deleteSource("api")
	if _, _, ok := getRegistryCreds("api"); ok {
		t.Fatal("deleting the project left its registry credentials behind")
	}
}

func TestImageProjectValidation(t *testing.T) {
	openTestDB(t)
	p := &Panel{}
	_ = putSource(Source{App: "taken", Repo: "acme/taken", Port: "3000"})
	for body, want := range map[string]int{
		`{"name":"Bad Name","image":"nginx"}`:             http.StatusBadRequest,
		`{"name":"web","image":"nginx latest"}`:           http.StatusBadRequest,
		`{"name":"web","image":"nginx","port":"99999"}`:   http.StatusBadRequest,
		`{"name":"web","image":"nginx","username":"bot"}`: http.StatusBadRequest,
		`{"name":"taken","image":"nginx"}`:                http.StatusConflict,
	} {
		w := httptest.NewRecorder()
		p.handleImageProject(w, httptest.NewRequest("POST", "/api/projects/image", strings.NewReader(body)))
		if w.Code != want {
			t.Errorf("%s → %d, want %d (%s)", body, w.Code, want, strings.TrimSpace(w.Body.String()))
		}
	}
	if got, _ := getSource("taken"); got.Image != "" {
		t.Fatal("an existing project was turned into an image project")
	}
}
