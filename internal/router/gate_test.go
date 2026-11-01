package router

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nuelScript/skiff/internal/docker"
	"github.com/nuelScript/skiff/internal/gate"
)

func gatedRouter(t *testing.T) *Router {
	t.Helper()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("app")) }))
	t.Cleanup(backend.Close)
	port, _ := strconv.Atoi(backend.URL[strings.LastIndexByte(backend.URL, ':')+1:])

	hash, _ := gate.Hash("letmein123")
	file := filepath.Join(t.TempDir(), "protected.json")
	if err := gate.Save(file, gate.Apps{"shop-feat": hash}); err != nil {
		t.Fatal(err)
	}
	rt := &Router{Domains: []string{"example.com"}, GateFile: file}
	rt.cachedRoutes = []docker.Route{{App: "shop-feat", HostPort: port}, {App: "shop", HostPort: port}}
	rt.cachedRouteAt = time.Now().Add(time.Hour)
	return rt
}

func do(rt *Router, method, target string, form url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
	var r *http.Request
	if form != nil {
		r = httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	rt.serve(w, r)
	return w
}

func TestPreviewPassword(t *testing.T) {
	rt := gatedRouter(t)

	if w := do(rt, "GET", "https://shop.example.com/", nil, nil); w.Body.String() != "app" {
		t.Fatalf("an unprotected app was gated: %d %q", w.Code, w.Body.String())
	}

	w := do(rt, "GET", "https://shop-feat.example.com/cart?x=1", nil, nil)
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "Password required") || !strings.Contains(w.Body.String(), `value="/cart?x=1"`) {
		t.Fatalf("expected the password page: %d %s", w.Code, w.Body.String())
	}
	if w := do(rt, "POST", "https://shop-feat.example.com/api/order", nil, nil); w.Code != http.StatusUnauthorized || strings.Contains(w.Body.String(), "app") {
		t.Fatalf("a locked POST reached the app: %d", w.Code)
	}

	if w := do(rt, "POST", "https://shop-feat.example.com"+unlockPath, url.Values{"password": {"nope"}, "next": {"/cart"}}, nil); w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "That password") {
		t.Fatalf("wrong password: %d", w.Code)
	}

	w = do(rt, "POST", "https://shop-feat.example.com"+unlockPath, url.Values{"password": {"letmein123"}, "next": {"/cart?x=1"}}, nil)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/cart?x=1" {
		t.Fatalf("right password: %d → %q", w.Code, w.Header().Get("Location"))
	}
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == gate.CookieName {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly {
		t.Fatalf("no http-only cookie set: %v", w.Result().Cookies())
	}
	if w := do(rt, "GET", "https://shop-feat.example.com/cart", nil, cookie); w.Body.String() != "app" {
		t.Fatalf("unlocked visitor was not let through: %d", w.Code)
	}
	cookie.Value += "0"
	if w := do(rt, "GET", "https://shop-feat.example.com/", nil, cookie); w.Code != http.StatusUnauthorized {
		t.Fatalf("tampered cookie accepted: %d", w.Code)
	}
}

func TestPreviewPasswordLimitsGuesses(t *testing.T) {
	rt := gatedRouter(t)
	wrong := url.Values{"password": {"guess"}}
	for i := 0; i < maxFailures; i++ {
		do(rt, "POST", "https://shop-feat.example.com"+unlockPath, wrong, nil)
	}
	w := do(rt, "POST", "https://shop-feat.example.com"+unlockPath, url.Values{"password": {"letmein123"}}, nil)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("guessing wasn't limited: %d", w.Code)
	}
}

func TestSafeNext(t *testing.T) {
	for in, want := range map[string]string{
		"/cart?x=1":               "/cart?x=1",
		"":                        "/",
		"https://evil.example":    "/",
		"//evil.example/":         "/",
		"/\\evil.example":         "/",
		unlockPath:                "/",
		"javascript:alert(1)":     "/",
		"/docs/getting-started#a": "/docs/getting-started#a",
	} {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGateFollowsTheFileImmediately(t *testing.T) {
	rt := gatedRouter(t)
	if w := do(rt, "GET", "https://shop.example.com/", nil, nil); w.Body.String() != "app" {
		t.Fatalf("shop should start open: %d", w.Code)
	}
	hash, _ := gate.Hash("letmein123")
	if err := gate.Save(rt.GateFile, gate.Apps{"shop-feat": hash, "shop": hash}); err != nil {
		t.Fatal(err)
	}
	if w := do(rt, "GET", "https://shop.example.com/", nil, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("a newly locked app was served open: %d", w.Code)
	}
}
