package panel

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nuelScript/skiff/internal/config"
)

// Every recipe has to be installable as written.
func TestCatalogRecipesAreValid(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range catalog {
		if seen[r.ID] || sanitizeName(r.ID) != r.ID {
			t.Errorf("%s: id must be unique and usable as an app name", r.ID)
		}
		seen[r.ID] = true
		if err := config.ValidateImage(r.Image); err != nil {
			t.Errorf("%s: %v", r.ID, err)
		}
		if strings.HasSuffix(r.Image, ":latest") || !strings.Contains(r.Image, ":") {
			t.Errorf("%s: pin the image to a version, so an install tomorrow matches what was tested", r.ID)
		}
		if err := config.ValidateVolumes(r.Volumes); err != nil {
			t.Errorf("%s: %v", r.ID, err)
		}
		if err := config.ValidateHealth("", r.Timeout); err != nil {
			t.Errorf("%s: %v", r.ID, err)
		}
		if r.Port == 0 || r.Setup == "" || r.Blurb == "" || r.Memory == "" {
			t.Errorf("%s: missing port, setup note, blurb or memory", r.ID)
		}
		if r.Database != "" {
			if _, ok := dbEngines[r.Database]; !ok {
				t.Errorf("%s: unknown database %q", r.ID, r.Database)
			}
			if _, ok := r.Env[r.DBVar]; !ok {
				t.Errorf("%s: dbVar %q isn't in its env", r.ID, r.DBVar)
			}
		}
		for k, v := range r.Env {
			if sanitizeEnvKey(k) != k {
				t.Errorf("%s: env key %q would be changed when saved", r.ID, k)
			}
			if r.Database == "" && strings.Contains(v, "{{db.") {
				t.Errorf("%s: %s uses a database it doesn't have", r.ID, k)
			}
		}
	}
}

func TestRenderRecipeEnv(t *testing.T) {
	n := 0
	secret := func() string { n++; return "s" + strconv.Itoa(n) }
	vars := renderRecipeEnv(map[string]string{
		"A": "{{secret}}", "B": "{{secret}}", "URL": "{{url}}/", "HOST": "{{host}}", "DB": "{{db.user}}:{{db.password}}", "PLAIN": "x",
	}, "https://blog.example.com", map[string]string{"user": "u", "password": "p"}, secret)
	got := map[string]string{}
	for _, v := range vars {
		got[v.Key] = v.Value
	}
	if got["A"] == got["B"] || !strings.HasPrefix(got["A"], "s") {
		t.Errorf("each secret should be its own: %q %q", got["A"], got["B"])
	}
	if got["URL"] != "https://blog.example.com/" || got["HOST"] != "blog.example.com" || got["DB"] != "u:p" || got["PLAIN"] != "x" {
		t.Errorf("rendered = %v", got)
	}
}

func TestCatalogRejects(t *testing.T) {
	openTestDB(t)
	p := &Panel{}
	_ = putSource(Source{App: "taken", Port: "80"})
	for body, want := range map[string]int{
		`{"id":"nope","name":"x"}`:       http.StatusBadRequest,
		`{"id":"n8n","name":"Bad Name"}`: http.StatusBadRequest,
		`{"id":"n8n","name":"taken"}`:    http.StatusConflict,
	} {
		w := httptest.NewRecorder()
		p.handleCatalog(w, httptest.NewRequest("POST", "/api/catalog", strings.NewReader(body)))
		if w.Code != want {
			t.Errorf("%s → %d, want %d", body, w.Code, want)
		}
	}
}

func TestCatalogNote(t *testing.T) {
	if catalogNote("umami") == "" || catalogNote("") != "" || catalogNote("gone") != "" {
		t.Fatal("notes should come from known recipes only")
	}
}

func TestProjectViewCarriesSetupNote(t *testing.T) {
	openTestDB(t)
	_ = putSource(Source{App: "stats", Team: "t1", Port: "3000", Image: "x:1", Catalog: "umami"})
	if got, _ := getSource("stats"); got.Catalog != "umami" || catalogNote(got.Catalog) == "" {
		t.Fatalf("catalog id didn't round-trip: %+v", got)
	}
}

func TestRecipeDBName(t *testing.T) {
	if got := recipeDBName("my-blog-2"); got != "my_blog_2" {
		t.Fatalf("recipeDBName = %q", got)
	}
}

func TestLoadCatalogRefusesMistakes(t *testing.T) {
	for name, body := range map[string]string{
		"unknown key": "name = \"X\"\nmemroy = \"1 GB\"\n",
		"bad toml":    "name = \n",
	} {
		_, err := loadCatalog(fstest.MapFS{"catalog/x.toml": {Data: []byte(body)}})
		if err == nil {
			t.Errorf("%s: loaded without an error", name)
		}
	}
	got, err := loadCatalog(fstest.MapFS{
		"catalog/b-app.toml": {Data: []byte("name = \"beta\"\nport = 80\n")},
		"catalog/a-app.toml": {Data: []byte("name = \"Alpha\"\n")},
	})
	if err != nil || len(got) != 2 || got[0].ID != "a-app" || got[1].ID != "b-app" || got[1].Port != 80 {
		t.Errorf("loadCatalog = %+v, %v", got, err)
	}
}
