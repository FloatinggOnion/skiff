package panel

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/nuelScript/skiff/internal/auth"
)

func domainRequest(p *Panel, body, session string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/domains", strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: p.cookieName(), Value: session})
	w := httptest.NewRecorder()
	p.handleDomains(w, req)
	return w
}

func stubTXT(t *testing.T, records map[string][]string) {
	t.Helper()
	_ = os.MkdirAll(skiffDir(), 0o755)
	old := lookupTXT
	lookupTXT = func(_ context.Context, name string) ([]string, error) { return records[name], nil }
	t.Cleanup(func() { lookupTXT = old })
}

func TestCustomDomainNeedsOwnershipProof(t *testing.T) {
	openTestDB(t)
	t.Setenv("HOME", t.TempDir())
	store := auth.NewStore(sqlDB)
	founder, founding, _ := store.CreateUser("founder@acme.dev", "Founder", "password123")
	guest, guestTeam, _ := store.CreateUser("guest@corp.dev", "Guest", "password123")
	putSource(Source{App: "site", Team: founding.ID, Repo: "a/site", Branch: "main", Port: "3000"})
	putSource(Source{App: "shop", Team: guestTeam.ID, Repo: "a/shop", Branch: "main", Port: "3000"})
	putSession("f", founder.ID, founding.ID)
	putSession("g", guest.ID, guestTeam.ID)
	p := &Panel{auth: store, domain: "skiff.test"}
	stubTXT(t, nil)

	if w := domainRequest(p, `{"app":"site","host":"acme.com"}`, "f"); w.Code != http.StatusOK {
		t.Fatalf("founding team is exempt: %d %s", w.Code, w.Body.String())
	}

	w := domainRequest(p, `{"app":"shop","host":"shop.corp.dev"}`, "g")
	want, _ := verifyValue(guestTeam.ID, "shop.corp.dev")
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "_skiff-verify.shop.corp.dev") || !strings.Contains(w.Body.String(), want) {
		t.Fatalf("want the record to publish, got %d %s", w.Code, w.Body.String())
	}

	other, _ := verifyValue(founding.ID, "shop.corp.dev")
	stubTXT(t, map[string][]string{"_skiff-verify.shop.corp.dev": {"v=spf1 -all", other}})
	if w := domainRequest(p, `{"app":"shop","host":"shop.corp.dev"}`, "g"); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("another team's value must not count: %d", w.Code)
	}

	stubTXT(t, map[string][]string{"_skiff-verify.shop.corp.dev": {want}})
	if w := domainRequest(p, `{"app":"shop","host":"shop.corp.dev"}`, "g"); w.Code != http.StatusOK {
		t.Fatalf("published record should pass: %d %s", w.Code, w.Body.String())
	}
}

func TestMailDomainNeedsProofAndCapsAliases(t *testing.T) {
	openTestDB(t)
	t.Setenv("HOME", t.TempDir())
	store := auth.NewStore(sqlDB)
	founder, founding, _ := store.CreateUser("founder@acme.dev", "Founder", "password123")
	guest, guestTeam, _ := store.CreateUser("guest@corp.dev", "Guest", "password123")
	putSession("f", founder.ID, founding.ID)
	putSession("g", guest.ID, guestTeam.ID)
	p := &Panel{auth: store, domain: "skiff.test"}
	stubTXT(t, nil)

	add := func(domain, alias, session string) *httptest.ResponseRecorder {
		return mailRequest(p, "POST", "/api/mail", fmt.Sprintf(`{"domain":%q,"alias":%q,"destinations":["x@y.dev"]}`, domain, alias), session)
	}
	if w := add("acme.com", "hi", "f"); w.Code != http.StatusOK {
		t.Fatalf("founding team exempt: %d %s", w.Code, w.Body.String())
	}
	if w := add("corp.dev", "hi", "g"); w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "_skiff-verify.corp.dev") {
		t.Fatalf("unproven domain: %d %s", w.Code, w.Body.String())
	}
	want, _ := verifyValue(guestTeam.ID, "corp.dev")
	stubTXT(t, map[string][]string{"_skiff-verify.corp.dev": {want}})
	if w := add("corp.dev", "hi", "g"); w.Code != http.StatusOK {
		t.Fatalf("proven domain: %d %s", w.Code, w.Body.String())
	}
	stubTXT(t, nil)
	for i := 1; i < maxAliasesPerDomain; i++ {
		if w := add("corp.dev", fmt.Sprintf("a%d", i), "g"); w.Code != http.StatusOK {
			t.Fatalf("alias %d: %d %s", i, w.Code, w.Body.String())
		}
	}
	if w := add("corp.dev", "one-too-many", "g"); w.Code != http.StatusBadRequest {
		t.Fatalf("alias cap: %d %s", w.Code, w.Body.String())
	}
}
