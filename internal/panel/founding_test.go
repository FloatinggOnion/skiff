package panel

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nuelScript/skiff/internal/auth"
)

func TestFoundingTeamCantPassToAnotherAccount(t *testing.T) {
	openTestDB(t)
	store := auth.NewStore(sqlDB)
	founder, founding, _ := store.CreateUser("founder@acme.dev", "Founder", "password123")
	guest, guestTeam, _ := store.CreateUser("guest@corp.dev", "Guest", "password123")
	_ = putSession("f", founder.ID, founding.ID)
	_ = putSession("g", guest.ID, guestTeam.ID)
	p := &Panel{auth: store, domain: "skiff.test"}

	if got := foundingTeam(); got != founding.ID {
		t.Fatalf("founding team is %q, want the first team", got)
	}
	post := func(h http.HandlerFunc, path, session, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: p.cookieName(), Value: session})
		w := httptest.NewRecorder()
		h(w, req)
		return w
	}
	if w := post(p.handleAccountDelete, "/api/account/delete", "f", `{"password":"password123"}`); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "runs the server") {
		t.Errorf("the founder's account was deletable while other teams exist: %d %s", w.Code, w.Body.String())
	}
	if _, ok := store.User(founder.ID); !ok {
		t.Fatal("the founder's account is gone")
	}
	// The founder also owns a second team, so only the founding-team rule can stop this.
	second, err := store.CreateTeam("second", founder.ID)
	if err != nil {
		t.Fatal(err)
	}
	if w := post(p.handleTeamDelete, "/api/teams/delete", "f", ""); w.Code != http.StatusBadRequest {
		t.Errorf("the founding team was deletable while others exist: %d", w.Code)
	}
	if got := foundingTeam(); got != founding.ID {
		t.Errorf("founding team moved to %q (second team is %q)", got, second.ID)
	}

	// Removing a guest's team doesn't touch it, and once the others are gone the founder is free to leave.
	if otherTeamsExist(guestTeam.ID) {
		t.Error("only the founding team is protected")
	}
}

func TestDeployKeepsItsFullCommit(t *testing.T) {
	openTestDB(t)
	addDeploy(Deploy{ID: "a", App: "shop", Commit: "abc1234", Status: "building", Started: 1})
	sha := strings.Repeat("a", 40)
	setDeploySHA("shop", "a", sha)
	var got string
	if err := sqlDB.QueryRow(`SELECT full_sha FROM deploys WHERE id='a'`).Scan(&got); err != nil || got != sha {
		t.Fatalf("got %q, %v", got, err)
	}
}
