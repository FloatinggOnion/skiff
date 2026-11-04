package panel

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nuelScript/skiff/internal/auth"
	"github.com/nuelScript/skiff/internal/mail"
)

func mailRequest(p *Panel, method, target, body, session string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: p.cookieName(), Value: session})
	w := httptest.NewRecorder()
	p.handleMail(w, req)
	return w
}

func TestMailForwardsValidateAndMirrorToRouter(t *testing.T) {
	openTestDB(t)
	t.Setenv("HOME", t.TempDir())

	store := auth.NewStore(sqlDB)
	owner, teamA, _ := store.CreateUser("owner@acme.dev", "Owner", "password123")
	other, teamB, _ := store.CreateUser("other@corp.dev", "Other", "password123")
	putSession("a", owner.ID, teamA.ID)
	putSession("b", other.ID, teamB.ID)
	inv, _ := store.CreateInvite("member@acme.dev", teamA.ID, auth.RoleMember)
	member, _ := store.CreateUserNoTeam("member@acme.dev", "Member", "password123")
	if _, err := store.AcceptInvite(inv.Token, member.ID); err != nil {
		t.Fatalf("AcceptInvite: %v", err)
	}
	putSession("m", member.ID, teamA.ID)
	p := &Panel{auth: store, domain: "skiff.test"}

	w := mailRequest(p, "POST", "/api/mail",
		`{"domain":"Acme.com.","alias":"Hello","destinations":["me@Gmail.com"," me@gmail.com ","team@corp.dev"]}`, "a")
	if w.Code != http.StatusOK {
		t.Fatalf("add forward: %d %s", w.Code, w.Body.String())
	}
	got := teamForwards(teamA.ID)
	if len(got) != 1 || got[0].Domain != "acme.com" || got[0].Alias != "hello" || len(got[0].Destinations) != 2 {
		t.Fatalf("forward not normalized/deduped: %+v", got)
	}

	for name, tc := range map[string]struct {
		body, session string
		want          int
	}{
		"duplicate alias":          {`{"domain":"acme.com","alias":"hello","destinations":["x@y.dev"]}`, "a", http.StatusConflict},
		"domain owned by a team":   {`{"domain":"acme.com","alias":"sales","destinations":["x@y.dev"]}`, "b", http.StatusConflict},
		"forward to itself":        {`{"domain":"acme.com","alias":"loop","destinations":["loop@acme.com"]}`, "a", http.StatusBadRequest},
		"bad destination":          {`{"domain":"acme.com","alias":"x","destinations":["not-an-email"]}`, "a", http.StatusBadRequest},
		"bad alias":                {`{"domain":"acme.com","alias":"a b","destinations":["x@y.dev"]}`, "a", http.StatusBadRequest},
		"plus tag alias":           {`{"domain":"acme.com","alias":"a+b","destinations":["x@y.dev"]}`, "a", http.StatusBadRequest},
		"no destinations":          {`{"domain":"acme.com","alias":"x","destinations":[" "]}`, "a", http.StatusBadRequest},
		"instance domain, member":  {`{"domain":"skiff.test","alias":"hi","destinations":["x@y.dev"]}`, "m", http.StatusForbidden},
		"instance subdomain, mem.": {`{"domain":"eu.skiff.test","alias":"hi","destinations":["x@y.dev"]}`, "m", http.StatusForbidden},
		// Anyone can create a team and own it, so owning *a* team mustn't grant the instance's own domain.
		"instance domain, owner of another team": {`{"domain":"skiff.test","alias":"hi","destinations":["x@y.dev"]}`, "b", http.StatusForbidden},
	} {
		if w := mailRequest(p, "POST", "/api/mail", tc.body, tc.session); w.Code != tc.want {
			t.Errorf("%s: want %d, got %d %s", name, tc.want, w.Code, w.Body.String())
		}
	}
	if w := mailRequest(p, "POST", "/api/mail", `{"domain":"skiff.test","alias":"*","destinations":["admin@corp.dev"]}`, "a"); w.Code != http.StatusOK {
		t.Fatalf("owner catch-all on the instance domain: %d %s", w.Code, w.Body.String())
	}

	if _, err := sqlDB.Exec(
		`INSERT INTO domains(host,app,team,parent,branch,created) VALUES('shop.corp.dev','shop',?,'','',0)`, teamB.ID); err != nil {
		t.Fatalf("insert web domain: %v", err)
	}
	for name, body := range map[string]string{
		"exact web domain":     `{"domain":"shop.corp.dev","alias":"hi","destinations":["x@y.dev"]}`,
		"parent of web domain": `{"domain":"corp.dev","alias":"hi","destinations":["x@y.dev"]}`,
		"under a web domain":   `{"domain":"mail.shop.corp.dev","alias":"hi","destinations":["x@y.dev"]}`,
	} {
		if w := mailRequest(p, "POST", "/api/mail", body, "a"); w.Code != http.StatusConflict {
			t.Errorf("%s: want 409, got %d %s", name, w.Code, w.Body.String())
		}
	}

	rules, err := mail.LoadRules(mailFilePath())
	if err != nil {
		t.Fatalf("router can't read the mirrored table: %v", err)
	}
	if dst, ok := rules.Lookup("hello+news@acme.com"); !ok || strings.Join(dst, ",") != "me@gmail.com,team@corp.dev" {
		t.Fatalf("router lookup through mirrored table: %v %v", dst, ok)
	}
	if dst, ok := rules.Lookup("anything@skiff.test"); !ok || dst[0] != "admin@corp.dev" {
		t.Fatalf("catch-all not mirrored: %v %v", dst, ok)
	}

	id := got[0].ID
	mailRequest(p, "DELETE", "/api/mail?id="+id, "", "b")
	if len(teamForwards(teamA.ID)) != 2 {
		t.Fatal("another team deleted a forward it doesn't own")
	}
	mailRequest(p, "DELETE", "/api/mail?id="+id, "", "a")
	rules, _ = mail.LoadRules(mailFilePath())
	if _, ok := rules.Lookup("hello@acme.com"); ok {
		t.Fatal("removed forward still in the router's table")
	}

	deleteTeamMail(teamA.ID)
	if rules, _ := mail.LoadRules(mailFilePath()); len(rules) != 0 {
		t.Fatalf("team deletion left forwards behind: %v", rules)
	}
}

func TestInstanceDomainBelongsToTheFoundingOwner(t *testing.T) {
	openTestDB(t)
	t.Setenv("HOME", t.TempDir())
	store := auth.NewStore(sqlDB)
	founder, founding, _ := store.CreateUser("founder@acme.dev", "Founder", "password123")
	putSession("f", founder.ID, founding.ID)
	p := &Panel{auth: store, domain: "skiff.test"}

	if w := mailRequest(p, "POST", "/api/mail", `{"domain":"skiff.test","alias":"hi","destinations":["x@y.dev"]}`, "f"); w.Code != http.StatusOK {
		t.Fatalf("founding owner refused their own instance domain: %d %s", w.Code, w.Body.String())
	}
	// A second team the founder also owns must not change who the instance belongs to.
	side, err := store.CreateTeam("Side project", founder.ID)
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	putSession("s", founder.ID, side.ID)
	if w := mailRequest(p, "POST", "/api/mail", `{"domain":"eu.skiff.test","alias":"hi","destinations":["x@y.dev"]}`, "s"); w.Code != http.StatusOK {
		t.Fatalf("the instance's owner should still qualify from another team: %d %s", w.Code, w.Body.String())
	}
}

func TestSPFAuthorizes(t *testing.T) {
	ip := "203.0.113.7"
	cases := []struct {
		txt    []string
		mxHere bool
		want   bool
	}{
		{[]string{"v=spf1 ip4:203.0.113.7 ~all"}, false, true},
		{[]string{"google-site-verification=x", "v=spf1 include:_spf.google.com +ip4:203.0.113.7/32 -all"}, false, true},
		{[]string{"v=spf1 mx ~all"}, true, true},
		{[]string{"v=spf1 mx ~all"}, false, false},
		{[]string{"v=spf1 ip4:203.0.113.0/24 -all"}, false, true},
		{[]string{"v=spf1 ip4:198.51.100.0/24 -all"}, false, false},
		{[]string{"v=spf1 ip4:203.0.113.70 ~all"}, false, false},
		{[]string{"ip4:203.0.113.7"}, false, false},
	}
	for _, c := range cases {
		if got := spfAuthorizes(c.txt, ip, c.mxHere); got != c.want {
			t.Errorf("spfAuthorizes(%v, mx=%v) = %v, want %v", c.txt, c.mxHere, got, c.want)
		}
	}
}
