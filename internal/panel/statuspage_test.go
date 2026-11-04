package panel

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nuelScript/skiff/internal/auth"
)

func publicStatus(p *Panel, slug string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	p.handlePublicStatus(w, httptest.NewRequest(http.MethodGet, "/public/status/"+slug, nil))
	return w
}

func TestPublicStatusShowsOnlyWhatWasChosen(t *testing.T) {
	openTestDB(t)
	p := &Panel{}
	now := time.Now()
	_ = putSource(Source{App: "shop", Team: "t1", Port: "80"})
	_ = putSource(Source{App: "secret-tool", Team: "t1", Port: "80"})
	_ = putSource(Source{App: "shop-feat-x", Team: "t1", Port: "80", Parent: "shop"})
	_ = putSource(Source{App: "other-team", Team: "t2", Port: "80"})
	_, _ = sqlDB.Exec(`INSERT INTO uptime_checks(app,t,ok,code,ms) VALUES('shop',?,1,200,12)`, now.Unix())
	_, _ = sqlDB.Exec(`INSERT INTO incidents(app,team,started,ended,cause) VALUES('shop','t1',?,?,'dial tcp 10.0.0.5:5432: refused')`, now.Add(-time.Hour).Unix(), now.Add(-50*time.Minute).Unix())

	// Off: indistinguishable from a page that doesn't exist.
	page := StatusPage{Team: "t1", Slug: "acme", Title: `Acme <script>`, Apps: []string{"shop", "shop-feat-x", "other-team"}}
	_ = saveStatusPage(page)
	if w := publicStatus(p, "acme"); w.Code != http.StatusNotFound {
		t.Fatalf("a disabled page answered %d", w.Code)
	}
	if off, none := publicStatus(p, "acme").Body.String(), publicStatus(p, "nobody").Body.String(); off != none {
		t.Fatal("a disabled page looks different from a missing one")
	}

	page.Enabled = true
	_ = saveStatusPage(page)
	publicStatusCache.clear("acme")
	w := publicStatus(p, "acme")
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, "shop") || !strings.Contains(body, "All systems operational") {
		t.Fatalf("%d %s", w.Code, body)
	}
	for _, leak := range []string{"secret-tool", "other-team", "shop-feat-x", "10.0.0.5", "dial tcp", "<script>"} {
		if strings.Contains(body, leak) {
			t.Errorf("the public page contains %q", leak)
		}
	}
	if !strings.Contains(body, "down for") {
		t.Error("the incident should still be listed, without its cause")
	}
}

func TestPublicStatusStates(t *testing.T) {
	openTestDB(t)
	now := time.Now()
	_ = putSource(Source{App: "a", Team: "t1", Port: "80"})
	_ = putSource(Source{App: "b", Team: "t1", Port: "80"})
	page := StatusPage{Team: "t1", Slug: "s", Enabled: true, Apps: []string{"a", "b"}}
	check := func(app string, at time.Time, ok int) {
		_, _ = sqlDB.Exec(`INSERT INTO uptime_checks(app,t,ok,code,ms) VALUES(?,?,?,200,1)`, app, at.Unix(), ok)
	}
	if d := buildStatusData(page, now); d.Headline != "Waiting for recent checks" {
		t.Fatalf("no data = %q", d.Headline)
	}
	check("a", now, 1)
	check("b", now, 0)
	if d := buildStatusData(page, now); d.Class != "partial" || d.Rows[1].Label != "Down" {
		t.Fatalf("one down = %+v", d)
	}
	check("a", now.Add(time.Second), 0)
	if d := buildStatusData(page, now.Add(time.Second)); d.Class != "major" {
		t.Fatalf("all down = %q", d.Class)
	}
	// Stale checks can't claim anything is up.
	check("a", now.Add(2*time.Second), 1)
	check("b", now.Add(2*time.Second), 1)
	if d := buildStatusData(page, now.Add(time.Hour)); d.Rows[0].Label != "No recent data" {
		t.Fatalf("stale = %+v", d.Rows[0])
	}
}

func TestStatusPageHandler(t *testing.T) {
	openTestDB(t)
	t.Setenv("HOME", t.TempDir())
	store := auth.NewStore(sqlDB)
	p := &Panel{auth: store, domain: "skiff.test"}
	owner, team, _ := store.CreateUser("o@acme.dev", "Owner", "password123")
	member, _, _ := store.CreateUser("m@acme.dev", "Member", "password123")
	_, _ = sqlDB.Exec(`INSERT INTO memberships(user_id,team_id,role) VALUES(?,?,'member')`, member.ID, team.ID)
	_ = putSession("o", owner.ID, team.ID)
	_ = putSession("m", member.ID, team.ID)
	_ = putSource(Source{App: "shop", Team: team.ID, Port: "80"})
	_ = putSource(Source{App: "shop-pr", Team: team.ID, Port: "80", Parent: "shop"})
	_ = putSource(Source{App: "theirs", Team: "other", Port: "80"})
	put := func(token, body string) int {
		req := httptest.NewRequest(http.MethodPut, "/api/status-page", strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: p.cookieName(), Value: token})
		w := httptest.NewRecorder()
		p.handleStatusPage(w, req)
		return w.Code
	}
	if c := put("m", `{"enabled":true}`); c != http.StatusForbidden {
		t.Fatalf("member: %d", c)
	}
	if c := put("o", `{"enabled":true,"title":"Acme","apps":["shop","shop-pr","theirs"]}`); c != http.StatusNoContent {
		t.Fatalf("save: %d", c)
	}
	s, _ := getStatusPage(team.ID)
	if len(s.Apps) != 1 || s.Apps[0] != "shop" || s.Slug == "" {
		t.Fatalf("page = %+v", s)
	}
	slug := s.Slug
	if c := put("o", `{"enabled":true,"host":"not a domain"}`); c != http.StatusBadRequest {
		t.Fatalf("bad host: %d", c)
	}
	if c := put("o", `{"enabled":true,"host":"status.skiff.test"}`); c != http.StatusBadRequest {
		t.Fatalf("a host under the served domain: %d", c)
	}
	if c := put("o", `{"enabled":true,"host":"status.acme.dev"}`); c != http.StatusNoContent {
		t.Fatalf("custom host: %d", c)
	}
	if got := statusDomains()["status.acme.dev"]; got != slug {
		t.Fatalf("statusDomains = %v", statusDomains())
	}
	if s, _ := getStatusPage(team.ID); s.Slug != slug {
		t.Fatal("the slug must not change once a link may be shared")
	}
	deleteStatusPage(team.ID)
	if _, ok := getStatusPage(team.ID); ok {
		t.Fatal("deleting the team left its page")
	}
}

func TestOnlyStatusPage(t *testing.T) {
	openTestDB(t)
	if _, ok := onlyStatusPage(); ok {
		t.Fatal("no pages")
	}
	_ = saveStatusPage(StatusPage{Team: "a", Slug: "a", Enabled: true})
	_ = saveStatusPage(StatusPage{Team: "off", Slug: "off"})
	if s, ok := onlyStatusPage(); !ok || s.Slug != "a" {
		t.Fatalf("one enabled page: %+v %v", s, ok)
	}
	_ = saveStatusPage(StatusPage{Team: "b", Slug: "b", Enabled: true})
	if _, ok := onlyStatusPage(); ok {
		t.Fatal("two pages: the bare address can't pick one")
	}
}
