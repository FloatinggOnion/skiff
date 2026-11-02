package panel

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nuelScript/skiff/internal/auth"
)

func TestDeleteAppDomainsKeepsManaged(t *testing.T) {
	openTestDB(t)
	now := time.Now().Unix()
	if _, err := sqlDB.Exec(
		`INSERT INTO domains(host,app,team,parent,branch,created) VALUES('app.example.com','myapp-staging','t1','','',?)`,
		now); err != nil {
		t.Fatalf("insert plain domain: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO domains(host,app,team,parent,branch,created) VALUES('staging.example.com','myapp-staging','t1','myapp','staging',?)`,
		now); err != nil {
		t.Fatalf("insert managed domain: %v", err)
	}

	if !deleteAppDomains("myapp-staging") {
		t.Fatal("expected the plain domain to be deleted (RowsAffected > 0)")
	}

	got := teamDomains("t1")
	if len(got) != 1 {
		t.Fatalf("want 1 surviving domain, got %d: %+v", len(got), got)
	}
	if d := got[0]; d.Host != "staging.example.com" || d.Parent != "myapp" || d.Branch != "staging" {
		t.Fatalf("managed branch domain not preserved intact: %+v", d)
	}

	if deleteAppDomains("myapp-staging") {
		t.Fatal("expected no further deletions on a managed-only app")
	}
}

func TestRebindBranchDomains(t *testing.T) {
	openTestDB(t)
	now := time.Now().Unix()
	if _, err := sqlDB.Exec(
		`INSERT INTO domains(host,app,team,parent,branch,created) VALUES('staging.acme.com','stale','t1','web','staging',?)`,
		now); err != nil {
		t.Fatalf("insert managed: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO domains(host,app,team,parent,branch,created) VALUES('acme.com','web','t1','','',?)`,
		now); err != nil {
		t.Fatalf("insert plain: %v", err)
	}

	if !rebindBranchDomains("web", "staging", "web-staging") {
		t.Fatal("expected the managed row to be rebound")
	}
	byHost := map[string]Domain{}
	for _, d := range teamDomains("t1") {
		byHost[d.Host] = d
	}
	if got := byHost["staging.acme.com"].App; got != "web-staging" {
		t.Fatalf("managed row not rebound: app=%q", got)
	}
	if got := byHost["acme.com"].App; got != "web" {
		t.Fatalf("plain domain wrongly touched: app=%q", got)
	}
	if rebindBranchDomains("web", "prod", "web-prod") {
		t.Fatal("expected no match for a branch with no bound domains")
	}
}

func TestAddBranchDomainBindsToPreview(t *testing.T) {
	openTestDB(t)
	t.Setenv("HOME", t.TempDir()) // isolate writeDomainsFile from the real ~/.skiff

	store := auth.NewStore(sqlDB)
	u, team, err := store.CreateUser("dev@acme.dev", "Dev", "password123")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	putSource(Source{App: "web", Team: team.ID, Repo: "acme/web", Branch: "main", Port: "3000"})
	putSession("s", u.ID, team.ID)

	p := &Panel{auth: store, domain: "skiff.test"}
	req := httptest.NewRequest("POST", "/api/domains",
		strings.NewReader(`{"app":"web","host":"staging.acme.com","branch":"staging"}`))
	req.AddCookie(&http.Cookie{Name: "skiff_session", Value: "s"})
	w := httptest.NewRecorder()
	p.handleDomains(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("POST branch domain: status %d, body %s", w.Code, w.Body.String())
	}
	got := teamDomains(team.ID)
	if len(got) != 1 {
		t.Fatalf("want 1 domain, got %d: %+v", len(got), got)
	}
	if d := got[0]; d.Host != "staging.acme.com" || d.App != "web-staging" || d.Parent != "web" || d.Branch != "staging" {
		t.Fatalf("branch domain not bound to the preview app: %+v", d)
	}

	req2 := httptest.NewRequest("POST", "/api/domains",
		strings.NewReader(`{"app":"ghost","host":"x.acme.com","branch":"staging"}`))
	req2.AddCookie(&http.Cookie{Name: "skiff_session", Value: "s"})
	w2 := httptest.NewRecorder()
	p.handleDomains(w2, req2)
	if w2.Code == http.StatusOK {
		t.Fatalf("expected rejection for a branch domain on an unknown project, got 200")
	}
}

func TestPrimaryDomain(t *testing.T) {
	for want, hosts := range map[string][]string{
		"":                {},
		"shop.com":        {"shop.com"},
		"acme.dev":        {"www.acme.dev", "acme.dev"},
		"shop.example.io": {"www.a.io", "shop.example.io"},
		"a.io":            {"b.io", "a.io", "longer.example.io"},
	} {
		if got := primaryDomain(hosts); got != want {
			t.Errorf("primaryDomain(%v) = %q, want %q", hosts, got, want)
		}
	}
}

func TestProjectDomains(t *testing.T) {
	openTestDB(t)
	for _, row := range [][]string{
		{"www.shop.com", "shop", "t1", "", ""},
		{"shop.com", "shop", "t1", "", ""},
		{"beta.shop.com", "shop-beta", "t1", "shop", "beta"},
		{"other.com", "other", "t2", "", ""},
	} {
		if _, err := sqlDB.Exec(`INSERT INTO domains(host,app,team,parent,branch,created) VALUES(?,?,?,?,?,1)`, row[0], row[1], row[2], row[3], row[4]); err != nil {
			t.Fatal(err)
		}
	}
	got := projectDomains("t1")
	if len(got) != 1 || len(got["shop"]) != 2 || got["shop"][0] != "shop.com" {
		t.Fatalf("domains = %v", got)
	}
}
