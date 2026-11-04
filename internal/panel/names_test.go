package panel

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReservedNames(t *testing.T) {
	_, founder, other := twoTeams(t)
	for _, n := range []string{"www", "dash", "status", "mx"} {
		if !nameReserved(n, other) {
			t.Errorf("%s should be reserved for other teams", n)
		}
		if nameReserved(n, founder) {
			t.Errorf("the founding team keeps using %s", n)
		}
	}
	if nameReserved("shop", other) {
		t.Error("ordinary names are free")
	}
	t.Setenv("SKIFF_SITE_APP", "landing")
	if !nameReserved("landing", other) || nameReserved("landing", founder) {
		t.Error("the configured site app is reserved like the others")
	}
}

func TestReservedNameRefusedOnCreate(t *testing.T) {
	p, _, _ := twoTeams(t)
	req := httptest.NewRequest("POST", "/api/deploy?name=www&git=https://203.0.113.7/a/b", nil)
	req.AddCookie(&http.Cookie{Name: "skiff_session", Value: "other"})
	w := httptest.NewRecorder()
	p.handleDeploy(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("another team deploying www: %d, want 400", w.Code)
	}
	if _, exists := getSource("www"); exists {
		t.Fatal("the reserved project was created")
	}
}
