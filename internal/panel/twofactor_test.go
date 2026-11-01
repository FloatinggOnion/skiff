package panel

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nuelScript/skiff/internal/auth"
)

func TestTwoFactorSignIn(t *testing.T) {
	openTestDB(t)
	store := auth.NewStore(sqlDB)
	p := &Panel{auth: store, domain: "skiff.test"}
	u, _, err := store.CreateUser("dev@acme.dev", "Dev", "password123")
	if err != nil {
		t.Fatal(err)
	}
	call := func(h http.HandlerFunc, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/", strings.NewReader(body))
		req.RemoteAddr = "203.0.113.9:4000"
		if cookie != nil {
			req.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h(w, req)
		return w
	}
	login := `{"email":"dev@acme.dev","password":"password123"}`

	w := call(p.handleLogin, login, nil)
	if w.Code != http.StatusNoContent || len(w.Result().Cookies()) != 1 {
		t.Fatalf("without two-factor the password should sign in: %d", w.Code)
	}
	session := w.Result().Cookies()[0]

	w = call(p.handleTwoFactor, `{"action":"setup","password":"wrong"}`, session)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("setup without the password: %d", w.Code)
	}
	w = call(p.handleTwoFactor, `{"action":"setup","password":"password123"}`, session)
	var setup struct{ Secret, URI string }
	_ = json.NewDecoder(w.Body).Decode(&setup)
	if w.Code != 200 || setup.Secret == "" || !strings.HasPrefix(setup.URI, "otpauth://totp/") {
		t.Fatalf("setup: %d %+v", w.Code, setup)
	}
	now := time.Now()
	w = call(p.handleTwoFactor, `{"action":"enable","code":"`+auth.CodeAt(setup.Secret, now)+`"}`, session)
	var enabled struct{ RecoveryCodes []string }
	_ = json.NewDecoder(w.Body).Decode(&enabled)
	if w.Code != 200 || len(enabled.RecoveryCodes) == 0 || !store.HasTwoFactor(u.ID) {
		t.Fatalf("enable: %d", w.Code)
	}

	w = call(p.handleLogin, login, nil)
	var step struct {
		TwoFactor bool
		Ticket    string
	}
	_ = json.NewDecoder(w.Body).Decode(&step)
	if w.Code != 200 || !step.TwoFactor || step.Ticket == "" || len(w.Result().Cookies()) != 0 {
		t.Fatalf("with two-factor the password alone must not sign in: %d %+v", w.Code, step)
	}
	if w := call(p.handleLoginCode, `{"ticket":"`+step.Ticket+`","code":"000000"}`, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong code: %d", w.Code)
	}
	if w := call(p.handleLoginCode, `{"ticket":"nope","code":"`+enabled.RecoveryCodes[0]+`"}`, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("unknown ticket: %d", w.Code)
	}
	w = call(p.handleLoginCode, `{"ticket":"`+step.Ticket+`","code":"`+enabled.RecoveryCodes[0]+`"}`, nil)
	if w.Code != http.StatusNoContent || len(w.Result().Cookies()) != 1 {
		t.Fatalf("recovery code: %d %s", w.Code, w.Body)
	}
	if w := call(p.handleLoginCode, `{"ticket":"`+step.Ticket+`","code":"`+enabled.RecoveryCodes[1]+`"}`, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("a used ticket was accepted again: %d", w.Code)
	}

	// Too many wrong codes spend the ticket.
	w = call(p.handleLogin, login, nil)
	_ = json.NewDecoder(w.Body).Decode(&step)
	for range ticketAttempts {
		call(p.handleLoginCode, `{"ticket":"`+step.Ticket+`","code":"000000"}`, nil)
	}
	loginLimiter.ok("203.0.113.9")
	if w := call(p.handleLoginCode, `{"ticket":"`+step.Ticket+`","code":"`+enabled.RecoveryCodes[2]+`"}`, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("ticket survived %d wrong codes: %d", ticketAttempts, w.Code)
	}

	if w := call(p.handleTwoFactor, `{"action":"disable","password":"password123","code":"000000"}`, session); w.Code != http.StatusUnauthorized {
		t.Fatalf("disable with a wrong code: %d", w.Code)
	}
	if w := call(p.handleTwoFactor, `{"action":"disable","password":"password123","code":"`+enabled.RecoveryCodes[3]+`"}`, session); w.Code != http.StatusNoContent || store.HasTwoFactor(u.ID) {
		t.Fatalf("disable: %d", w.Code)
	}
}
