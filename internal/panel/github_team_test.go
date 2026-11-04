package panel

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nuelScript/skiff/internal/auth"
)

func twoTeams(t *testing.T) (*Panel, string, string) {
	t.Helper()
	openTestDB(t)
	store := auth.NewStore(sqlDB)
	u1, founder, err := store.CreateUser("a@acme.dev", "A", "password123")
	if err != nil {
		t.Fatal(err)
	}
	u2, other, err := store.CreateUser("b@acme.dev", "B", "password123")
	if err != nil {
		t.Fatal(err)
	}
	putSession("founder", u1.ID, founder.ID)
	putSession("other", u2.ID, other.ID)
	return &Panel{auth: store}, founder.ID, other.ID
}

func ghDeploy(p *Panel, session, query string) int {
	req := httptest.NewRequest("POST", "/api/github/deploy?"+query, nil)
	req.AddCookie(&http.Cookie{Name: "skiff_session", Value: session})
	w := httptest.NewRecorder()
	p.handleGithubDeploy(w, req)
	return w.Code
}

func TestGithubDeployRefusesOtherTeamsAndMismatchedClones(t *testing.T) {
	p, founder, other := twoTeams(t)
	if !githubUsable(founder) || githubUsable(other) || githubUsable("") {
		t.Fatalf("only the founding team may use the App")
	}
	if got := ghDeploy(p, "other", "repo=acme/api&name=api"); got != http.StatusForbidden {
		t.Errorf("another team: %d, want 403", got)
	}
	for _, q := range []string{
		"repo=acme/api&name=api&clone=https://evil.example/acme/api.git",
		"repo=acme/api&name=api&clone=https://github.com/acme/other.git",
		"repo=acme&name=api",
		"repo=../x&name=api",
		"repo=acme/api&name=api&port=3000%0A%5Bbuild%5D",
		"repo=acme/api&name=api&port=70000",
	} {
		if got := ghDeploy(p, "founder", q); got != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", q, got)
		}
	}
}

func TestPushesOnlyReachTheFoundingTeam(t *testing.T) {
	_, founder, other := twoTeams(t)
	for _, s := range []Source{
		{App: "mine", Team: founder, Repo: "acme/api", Auto: true, Port: "3000"},
		{App: "theirs", Team: other, Repo: "acme/api", Auto: true, Port: "3000"},
	} {
		if err := putSource(s); err != nil {
			t.Fatal(err)
		}
	}
	got := sourcesForRepo("acme/api", "main")
	if len(got) != 1 || got[0].App != "mine" {
		t.Fatalf("sourcesForRepo = %+v", got)
	}
	if got := productionAppsForRepo("acme/api"); len(got) != 1 || got[0].App != "mine" {
		t.Fatalf("productionAppsForRepo = %+v", got)
	}
}
