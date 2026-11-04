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

func req(p *Panel, method, target, body, token string, hdr map[string]string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	if token != "" {
		r.AddCookie(&http.Cookie{Name: p.cookieName(), Value: token})
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	return r
}

func TestCrossSiteRequestsAreRefused(t *testing.T) {
	openTestDB(t)
	store := auth.NewStore(sqlDB)
	u, team, _ := store.CreateUser("dev@acme.dev", "Dev", "password123")
	_ = putSession("s", u.ID, team.ID)
	p := &Panel{auth: store, domain: "skiff.test"}
	h := p.protected(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })

	cases := []struct {
		name string
		hdr  map[string]string
		want int
	}{
		{"no browser headers", nil, 204},
		{"same origin fetch", map[string]string{"Sec-Fetch-Site": "same-origin"}, 204},
		{"address bar", map[string]string{"Sec-Fetch-Site": "none"}, 204},
		{"sibling subdomain", map[string]string{"Sec-Fetch-Site": "same-site"}, 403},
		{"other site", map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
		{"origin matches host", map[string]string{"Origin": "https://example.com"}, 204},
		{"origin is a sibling", map[string]string{"Origin": "https://app.example.com"}, 403},
		{"opaque origin", map[string]string{"Origin": "null"}, 403},
	}
	for _, c := range cases {
		r := req(p, "POST", "https://example.com/api/x", "", "s", c.hdr)
		w := httptest.NewRecorder()
		h(w, r)
		if w.Code != c.want {
			t.Errorf("%s: %d, want %d", c.name, w.Code, c.want)
		}
	}

	// A state-changing GET can't be reached by navigating to it, but the dashboard's EventSource still can.
	nav := req(p, "GET", "/api/redeploy?app=x", "", "s", map[string]string{"Sec-Fetch-Site": "same-origin", "Sec-Fetch-Mode": "navigate"})
	w := httptest.NewRecorder()
	h(w, nav)
	if w.Code != 403 {
		t.Errorf("navigating to a deploy route: %d, want 403", w.Code)
	}
	es := req(p, "GET", "/api/redeploy?app=x", "", "s", map[string]string{"Sec-Fetch-Site": "same-origin", "Sec-Fetch-Mode": "cors"})
	w = httptest.NewRecorder()
	h(w, es)
	if w.Code != 204 {
		t.Errorf("event stream to a deploy route: %d, want 204", w.Code)
	}

	// The app-setup redirect arrives from another site as a top-level navigation.
	cb := p.protectedCallback(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	w = httptest.NewRecorder()
	cb(w, req(p, "GET", "/api/github/created?code=1", "", "s", map[string]string{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "navigate"}))
	if w.Code != 204 {
		t.Errorf("callback navigation: %d, want 204", w.Code)
	}
	w = httptest.NewRecorder()
	cb(w, req(p, "POST", "/api/github/created", "", "s", map[string]string{"Sec-Fetch-Site": "cross-site"}))
	if w.Code != 403 {
		t.Errorf("callback cross-site post: %d, want 403", w.Code)
	}

	w = httptest.NewRecorder()
	cb(w, req(p, "GET", "/api/github/created?code=1", "", "s", map[string]string{"Sec-Fetch-Site": "same-site", "Sec-Fetch-Mode": "navigate"}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("a sibling subdomain reached a callback: %d", w.Code)
	}
}

func TestSessionCookieNames(t *testing.T) {
	openTestDB(t)
	store := auth.NewStore(sqlDB)
	u, team, _ := store.CreateUser("dev@acme.dev", "Dev", "password123")

	https := &Panel{auth: store, domain: "skiff.test"}
	w := httptest.NewRecorder()
	if err := https.setSession(w, u.ID, team.ID); err != nil {
		t.Fatal(err)
	}
	c := w.Result().Cookies()[0]
	if c.Name != "__Host-skiff_session" || !c.Secure || c.Path != "/" || c.Domain != "" {
		t.Fatalf("https cookie = %+v", c)
	}
	// A plain-named cookie, as a sibling subdomain could plant, is never honoured over https.
	plain := httptest.NewRequest("GET", "/api/me", nil)
	plain.AddCookie(&http.Cookie{Name: "skiff_session", Value: c.Value})
	if _, ok := https.session(plain); ok {
		t.Fatal("https panel accepted the plain cookie")
	}
	host := httptest.NewRequest("GET", "/api/me", nil)
	host.AddCookie(c)
	if _, ok := https.session(host); !ok {
		t.Fatal("https panel refused its own cookie")
	}

	local := &Panel{auth: store, domain: "localhost"}
	w = httptest.NewRecorder()
	_ = local.setSession(w, u.ID, team.ID)
	lc := w.Result().Cookies()[0]
	if lc.Name != "skiff_session" || lc.Secure {
		t.Fatalf("local cookie = %+v", lc)
	}
	prefixed := httptest.NewRequest("GET", "/api/me", nil)
	prefixed.AddCookie(&http.Cookie{Name: "__Host-skiff_session", Value: lc.Value})
	if _, ok := local.session(prefixed); ok {
		t.Fatal("local panel accepted the __Host- cookie")
	}
}

func TestSecureHeaders(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/strict" {
			w.Header().Set("Content-Security-Policy", "sandbox")
		}
	})
	for _, c := range []struct {
		domain string
		hsts   bool
	}{{"skiff.test", true}, {"localhost", false}} {
		p := &Panel{domain: c.domain}
		w := httptest.NewRecorder()
		p.secureHeaders(next).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
		h := w.Header()
		if h.Get("X-Frame-Options") != "DENY" || h.Get("X-Content-Type-Options") != "nosniff" ||
			h.Get("Referrer-Policy") != "same-origin" ||
			h.Get("Content-Security-Policy") != "frame-ancestors 'none'; base-uri 'self'; object-src 'none'" {
			t.Fatalf("%s headers = %v", c.domain, h)
		}
		if got := h.Get("Strict-Transport-Security"); (got == "max-age=31536000") != c.hsts || strings.Contains(got, "includeSubDomains") {
			t.Fatalf("%s HSTS = %q", c.domain, got)
		}
		w = httptest.NewRecorder()
		p.secureHeaders(next).ServeHTTP(w, httptest.NewRequest("GET", "/strict", nil))
		if w.Header().Get("Content-Security-Policy") != "sandbox" {
			t.Fatal("a handler's own stricter policy was overwritten")
		}
	}
}

func TestRemovedMemberLosesTheTeam(t *testing.T) {
	openTestDB(t)
	store := auth.NewStore(sqlDB)
	owner, team, _ := store.CreateUser("owner@acme.dev", "Owner", "password123")
	member, _, _ := store.CreateUser("m@acme.dev", "M", "password123")
	if _, err := sqlDB.Exec(`INSERT INTO memberships(user_id,team_id,role) VALUES(?,?,'member')`, member.ID, team.ID); err != nil {
		t.Fatal(err)
	}
	_ = putSession("o", owner.ID, team.ID)
	_ = putSession("m", member.ID, team.ID)
	p := &Panel{auth: store, domain: "skiff.test"}
	tok, err := createToken(team.ID, "ci", member.ID)
	if err != nil {
		t.Fatal(err)
	}

	if got := p.teamID(req(p, "GET", "/", "", "m", nil)); got != team.ID {
		t.Fatalf("member's team = %q", got)
	}
	// Even a stale session row naming the team gets no team once the membership is gone.
	if _, err := sqlDB.Exec(`DELETE FROM memberships WHERE user_id = ?`, member.ID); err != nil {
		t.Fatal(err)
	}
	if got := p.teamID(req(p, "GET", "/", "", "m", nil)); got != "" {
		t.Fatalf("removed member still has team %q", got)
	}
	if _, err := sqlDB.Exec(`INSERT INTO memberships(user_id,team_id,role) VALUES(?,?,'member')`, member.ID, team.ID); err != nil {
		t.Fatal(err)
	}

	if err := store.RemoveMember(team.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	if s, _ := getSession("m"); s.teamID != "" {
		t.Fatalf("session still points at %q", s.teamID)
	}
	if _, _, ok := resolveToken(tok.Token); ok {
		t.Fatal("removed member's token still works")
	}
	if p.isOwner(req(p, "GET", "/", "", "o", nil)) != true {
		t.Fatal("owner lost access")
	}
}

func TestInstanceRoutesNeedTheInstanceAdmin(t *testing.T) {
	openTestDB(t)
	t.Setenv("HOME", t.TempDir())
	store := auth.NewStore(sqlDB)
	founder, founding, _ := store.CreateUser("founder@acme.dev", "Founder", "password123")
	other, otherTeam, _ := store.CreateUser("other@corp.dev", "Other", "password123")
	_ = putSession("f", founder.ID, founding.ID)
	_ = putSession("o", other.ID, otherTeam.ID) // owns their own team, but not the instance
	p := &Panel{auth: store, domain: "skiff.test"}

	routes := map[string]http.HandlerFunc{
		"server":    p.handleServer,
		"usage":     p.handleServerUsage,
		"create":    p.handleGithubCreate,
		"created":   p.handleGithubCreated,
		"installed": p.handleGithubInstalled,
	}
	for name, h := range routes {
		w := httptest.NewRecorder()
		h(w, req(p, "GET", "/api/x?code=c", "", "o", nil))
		if w.Code != http.StatusForbidden {
			t.Errorf("%s as a team owner: %d, want 403", name, w.Code)
		}
	}
	w := httptest.NewRecorder()
	p.handleGithubCreate(w, req(p, "GET", "/api/github/create", "", "f", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "github.com/settings/apps/new") {
		t.Errorf("create as the instance admin: %d", w.Code)
	}
}

func TestCLIApprovalNeedsTheTypedCode(t *testing.T) {
	openTestDB(t)
	store := auth.NewStore(sqlDB)
	u, team, _ := store.CreateUser("dev@acme.dev", "Dev", "password123")
	_ = putSession("s", u.ID, team.ID)
	p := &Panel{auth: store, domain: "skiff.test"}
	cliLogins = map[string]*cliLogin{}
	cliCodeLimiter.ok(u.ID)

	w := httptest.NewRecorder()
	start := req(p, "POST", "https://skiff.test/api/cli/login", `{"host":"laptop"}`, "", nil)
	start.RemoteAddr = "203.0.113.9:4000"
	p.handleCLILogin(w, start)
	var got struct{ Code, Poll, ApproveURL string }
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Code == "" || strings.Contains(got.ApproveURL, got.Code) || strings.Contains(got.ApproveURL, "?") {
		t.Fatalf("approve url carries the code: %+v", got)
	}

	w = httptest.NewRecorder()
	p.handleCLIApprove(w, req(p, "GET", "/api/cli/approve?code="+got.Code, "", "s", nil))
	var shown struct {
		Host, IP  string
		Requested int64
	}
	_ = json.Unmarshal(w.Body.Bytes(), &shown)
	if w.Code != 200 || shown.Host != "laptop" || shown.IP != "203.0.113.9" || shown.Requested == 0 {
		t.Fatalf("pending = %d %+v", w.Code, shown)
	}
	// Viewing it approves nothing.
	if l := cliLogins[got.Poll]; l.token != "" {
		t.Fatal("a GET approved the sign-in")
	}

	for i := 0; i < 5; i++ {
		w = httptest.NewRecorder()
		p.handleCLIApprove(w, req(p, "POST", "/api/cli/approve?code=00000000", "", "s", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("wrong code %d: %d", i, w.Code)
		}
	}
	w = httptest.NewRecorder()
	p.handleCLIApprove(w, req(p, "POST", "/api/cli/approve?code="+got.Code, "", "s", nil))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("after five wrong codes, even the right one: %d, want 429", w.Code)
	}

	cliCodeLimiter.ok(u.ID)
	w = httptest.NewRecorder()
	p.handleCLIApprove(w, req(p, "POST", "/api/cli/approve?code="+got.Code, "", "s", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("approve: %d", w.Code)
	}
	var by string
	_ = sqlDB.QueryRow(`SELECT created_by FROM api_tokens WHERE team = ?`, team.ID).Scan(&by)
	if by != u.ID {
		t.Fatalf("token created_by = %q, want %q", by, u.ID)
	}
}

func TestSecondFactorLockout(t *testing.T) {
	openTestDB(t)
	store := auth.NewStore(sqlDB)
	u, _, _ := store.CreateUser("dev@acme.dev", "Dev", "password123")
	p := &Panel{auth: store}
	factorLimiter.ok(u.ID)

	for i := 0; i < factorMaxFails; i++ {
		if _, locked := p.checkFactor(u.ID, "000000"); locked {
			t.Fatalf("locked after only %d failures", i)
		}
	}
	if _, locked := p.checkFactor(u.ID, "000000"); !locked {
		t.Fatal("not locked after 10 failures")
	}
	// A correct password elsewhere doesn't lift it; time does.
	factorLimiter.mu.Lock()
	factorLimiter.fails[u.ID].until = time.Now().Add(-time.Second)
	factorLimiter.mu.Unlock()
	if _, locked := p.checkFactor(u.ID, "000000"); locked {
		t.Fatal("still locked after the lockout ended")
	}
	factorLimiter.ok(u.ID)
}

func TestCredentialChangesEndOtherSessionsAndDeleteNeedsCode(t *testing.T) {
	openTestDB(t)
	store := auth.NewStore(sqlDB)
	u, team, _ := store.CreateUser("dev@acme.dev", "Dev", "password123")
	_ = putSession("here", u.ID, team.ID)
	_ = putSession("elsewhere", u.ID, team.ID)
	p := &Panel{auth: store, domain: "skiff.test"}

	w := httptest.NewRecorder()
	p.handlePassword(w, req(p, "POST", "/api/account/password", `{"current":"password123","password":"newpassword456"}`, "here", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("password change: %d %s", w.Code, w.Body.String())
	}
	if _, ok := getSession("elsewhere"); ok {
		t.Fatal("other session survived a password change")
	}
	if _, ok := getSession("here"); !ok {
		t.Fatal("the current session was ended")
	}

	if _, err := sqlDB.Exec(`UPDATE users SET totp_secret = 'JBSWY3DPEHPK3PXP' WHERE id = ?`, u.ID); err != nil {
		t.Fatal(err)
	}
	factorLimiter.ok(u.ID)
	w = httptest.NewRecorder()
	p.handleAccountDelete(w, req(p, "POST", "/api/account/delete", `{"password":"newpassword456"}`, "here", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("delete without a code: %d, want 401", w.Code)
	}
	if _, ok := store.User(u.ID); !ok {
		t.Fatal("account deleted without its second factor")
	}
	factorLimiter.ok(u.ID)
}

func TestAlertsAreOwnerOnlyAndMaskedForMembers(t *testing.T) {
	openTestDB(t)
	store := auth.NewStore(sqlDB)
	owner, team, _ := store.CreateUser("owner@acme.dev", "Owner", "password123")
	member, _, _ := store.CreateUser("m@acme.dev", "M", "password123")
	if _, err := sqlDB.Exec(`INSERT INTO memberships(user_id,team_id,role) VALUES(?,?,'member')`, member.ID, team.ID); err != nil {
		t.Fatal(err)
	}
	_ = putSession("o", owner.ID, team.ID)
	_ = putSession("m", member.ID, team.ID)
	p := &Panel{auth: store, domain: "skiff.test"}
	secret := "https://hooks.slack.com/services/T000/B000/s3cr3t"

	w := httptest.NewRecorder()
	p.handleAlerts(w, req(p, "PUT", "/api/alerts", `{"slackUrl":"`+secret+`"}`, "m", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("member PUT: %d, want 403", w.Code)
	}
	w = httptest.NewRecorder()
	p.handleAlerts(w, req(p, "PUT", "/api/alerts", `{"slackUrl":"`+secret+`"}`, "o", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("owner PUT: %d", w.Code)
	}

	w = httptest.NewRecorder()
	p.handleAlerts(w, req(p, "GET", "/api/alerts", "", "m", nil))
	if strings.Contains(w.Body.String(), "s3cr3t") || !strings.Contains(w.Body.String(), "hooks.slack.com") {
		t.Fatalf("member sees %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	p.handleAlerts(w, req(p, "GET", "/api/alerts", "", "o", nil))
	if !strings.Contains(w.Body.String(), "s3cr3t") {
		t.Fatalf("owner sees %s", w.Body.String())
	}
}
