package panel

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nuelScript/skiff/internal/auth"
	"github.com/nuelScript/skiff/internal/db"
)

func openTestDB(t *testing.T) {
	t.Helper()
	database, err := db.OpenAt(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.OpenAt: %v", err)
	}
	sqlDB = database
}

func TestSourceRoundTrip(t *testing.T) {
	openTestDB(t)
	in := Source{App: "api", Team: "t1", Repo: "acme/api", Branch: "main", Port: "3000",
		Replicas: 3, Autoscale: true, ScaleMin: 2, ScaleMax: 6, ScaleCPU: 60}
	if err := putSource(in); err != nil {
		t.Fatalf("putSource: %v", err)
	}
	got, ok := getSource("api")
	if !ok {
		t.Fatal("getSource missing after put")
	}
	if got.Team != "t1" || got.Replicas != 3 || !got.Autoscale || got.ScaleMax != 6 || got.ScaleCPU != 60 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestSessionExpiry(t *testing.T) {
	openTestDB(t)
	sqlDB.Exec(`INSERT INTO sessions(token,user_id,team_id,created) VALUES('fresh','u','t',?)`, time.Now().Unix())
	sqlDB.Exec(`INSERT INTO sessions(token,user_id,team_id,created) VALUES('stale','u','t',?)`, time.Now().Unix()-sessionMaxAge-1)
	if _, ok := getSession("fresh"); !ok {
		t.Fatal("fresh session rejected")
	}
	if _, ok := getSession("stale"); ok {
		t.Fatal("expired session (>30d) still accepted")
	}
}

func TestDeployFeedTeamScoped(t *testing.T) {
	openTestDB(t)
	store := auth.NewStore(sqlDB)
	u, team, err := store.CreateUser("dev@acme.dev", "Dev", "password123")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	putSource(Source{App: "mine", Team: team.ID, Port: "3000"})
	putSource(Source{App: "theirs", Team: "other", Port: "3000"})
	addDeploy(Deploy{ID: "d1", App: "mine", Status: "live", Started: 3})
	addDeploy(Deploy{ID: "d2", App: "theirs", Status: "live", Started: 2})
	addDeploy(Deploy{ID: "d3", App: "panel", Status: "live", Started: 1})
	putSession("s", u.ID, team.ID)

	p := &Panel{auth: store}
	req := httptest.NewRequest("GET", "/api/deploys", nil)
	req.AddCookie(&http.Cookie{Name: "skiff_session", Value: "s"})
	w := httptest.NewRecorder()
	p.handleDeploys(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "mine") || strings.Contains(body, "theirs") {
		t.Fatalf("deploy feed leaked another team's builds or dropped ours: %s", body)
	}
	if !strings.Contains(body, "panel") {
		t.Fatalf("deploy feed hid the control plane: %s", body)
	}
	if !p.canViewDeploys(req, "panel") {
		t.Fatal("control plane (panel) deploys wrongly forbidden")
	}
	if p.canViewDeploys(req, "theirs") {
		t.Fatal("another team's deploys wrongly allowed")
	}
}

func TestTeamDeploysPagination(t *testing.T) {
	openTestDB(t)
	putSource(Source{App: "mine", Team: "t1", Port: "3000"})
	putSource(Source{App: "theirs", Team: "other", Port: "3000"})
	for _, d := range []Deploy{
		{ID: "m0", App: "mine", Status: "live", Started: 100},
		{ID: "m1", App: "mine", Status: "live", Started: 102},
		{ID: "m2", App: "mine", Status: "live", Started: 104},
		{ID: "m3", App: "mine", Status: "live", Started: 106},
		{ID: "m4", App: "mine", Status: "live", Started: 108},
		{ID: "o0", App: "theirs", Status: "live", Started: 103},
		{ID: "o1", App: "theirs", Status: "live", Started: 105},
		{ID: "p0", App: controlPlaneApp, Status: "live", Started: 200},
	} {
		addDeploy(d)
	}

	p1 := teamDeploys("t1", 0, "", 3)
	if len(p1) != 3 || p1[0].ID != "p0" || p1[1].ID != "m4" || p1[2].ID != "m3" {
		t.Fatalf("page1 wrong: %+v", p1)
	}

	last := p1[len(p1)-1]
	p2 := teamDeploys("t1", last.Started, last.ID, 3)
	seen := map[string]bool{}
	for _, d := range append(p1, p2...) {
		if d.App == "theirs" {
			t.Fatalf("leaked another team's deploy: %+v", d)
		}
		if seen[d.ID] {
			t.Fatalf("row %s appears on both pages", d.ID)
		}
		seen[d.ID] = true
	}
	if len(seen) != 6 { // 5 mine + control plane, never the 2 "theirs"
		t.Fatalf("want 6 reachable rows, got %d: %v", len(seen), seen)
	}

	oldest := p2[len(p2)-1]
	if extra := teamDeploys("t1", oldest.Started, oldest.ID, 3); len(extra) != 0 {
		t.Fatalf("expected nothing past the end, got %+v", extra)
	}
}

func TestPreviewEnvLayers(t *testing.T) {
	openTestDB(t)
	if _, err := sqlDB.Exec(`INSERT INTO shared_env(team,key,value,build) VALUES('t1','REGION','eu',0),('t1','DATABASE_URL','shared-db',0)`); err != nil {
		t.Fatal(err)
	}
	prod := Source{App: "shop", Team: "t1"}
	preview := Source{App: "shop-feat", Team: "t1", Parent: "shop"}
	_ = setEnv("shop", []EnvVar{{Key: "DATABASE_URL", Value: "prod-db"}, {Key: "API_KEY", Value: "live"}})
	_ = setPreviewEnv("shop", []EnvVar{{Key: "DATABASE_URL", Value: "staging-db"}})
	_ = setEnv("shop-feat", []EnvVar{{Key: "API_KEY", Value: "test-only-this-branch"}})

	env := func(src Source) map[string]string {
		out := map[string]string{}
		for _, e := range deployEnv(src) {
			out[e.Key] = e.Value
		}
		return out
	}
	if got := env(prod); got["DATABASE_URL"] != "prod-db" || got["API_KEY"] != "live" || got["REGION"] != "eu" {
		t.Errorf("production must ignore preview overrides: %v", got)
	}
	if got := env(preview); got["DATABASE_URL"] != "staging-db" || got["API_KEY"] != "test-only-this-branch" || got["REGION"] != "eu" {
		t.Errorf("preview layering wrong: %v", got)
	}

	// A change to the project's own vars reaches the preview on its next deploy.
	_ = setEnv("shop", []EnvVar{{Key: "DATABASE_URL", Value: "prod-db"}, {Key: "FEATURE", Value: "on"}})
	if got := env(preview); got["FEATURE"] != "on" {
		t.Errorf("preview missed a new project var: %v", got)
	}

	deleteSource("shop")
	if len(getPreviewEnv("shop")) != 0 {
		t.Error("deleting the project left its preview overrides behind")
	}
}

func TestDropCopiedPreviewEnv(t *testing.T) {
	openTestDB(t)
	_ = putSource(Source{App: "shop"})
	_ = putSource(Source{App: "shop-feat", Parent: "shop"})
	_ = setEnv("shop", []EnvVar{{Key: "DATABASE_URL", Value: "prod-db"}, {Key: "A", Value: "1"}})
	_ = setEnv("shop-feat", []EnvVar{{Key: "DATABASE_URL", Value: "prod-db"}, {Key: "A", Value: "1"}, {Key: "ONLY_HERE", Value: "x"}})
	_ = setPreviewEnv("shop", []EnvVar{{Key: "DATABASE_URL", Value: "staging-db"}})

	dropCopiedPreviewEnv()

	if got := getEnv("shop-feat"); len(got) != 1 || got[0].Key != "ONLY_HERE" {
		t.Fatalf("only the preview's own var should remain, got %v", got)
	}
	if len(getEnv("shop")) != 2 {
		t.Fatal("the project's own vars were touched")
	}
	for _, e := range deployEnv(Source{App: "shop-feat", Parent: "shop"}) {
		if e.Key == "DATABASE_URL" && e.Value != "staging-db" {
			t.Fatalf("the override should now win, got %q", e.Value)
		}
	}
}
