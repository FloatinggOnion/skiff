package panel

import (
	"cmp"
	"context"
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	netmail "net/mail"
	"strings"
	"time"

	"github.com/nuelScript/skiff/internal/auth"
	"github.com/nuelScript/skiff/internal/mail"
)

type userView struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

type meResponse struct {
	Authenticated bool        `json:"authenticated"`
	NeedsSetup    bool        `json:"needsSetup"`
	User          *userView   `json:"user,omitempty"`
	Teams         []auth.Team `json:"teams,omitempty"`
	Team          string      `json:"team,omitempty"`
	Role          string      `json:"role,omitempty"`
	Admin         bool        `json:"admin,omitempty"`
}

func (p *Panel) handleMe(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	s, ok := p.session(r)
	if !ok {
		_ = json.NewEncoder(w).Encode(meResponse{NeedsSetup: !p.auth.HasUsers()})
		return
	}
	u, _ := p.auth.User(s.userID)
	role, _ := p.auth.Role(s.userID, s.teamID)
	_ = json.NewEncoder(w).Encode(meResponse{
		Authenticated: true,
		User:          &userView{ID: u.ID, Email: u.Email, Name: u.Name},
		Teams:         p.auth.TeamsForUser(s.userID),
		Team:          s.teamID,
		Role:          role,
		Admin:         p.isInstanceAdmin(r),
	})
}

func (p *Panel) handleSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if p.auth.HasUsers() {
		http.Error(w, "already set up", http.StatusConflict)
		return
	}
	var body struct{ Secret, Email, Name, Password string }
	_ = json.NewDecoder(r.Body).Decode(&body)
	if subtle.ConstantTimeCompare([]byte(body.Secret), []byte(p.setupSecret)) != 1 {
		http.Error(w, "wrong setup secret", http.StatusUnauthorized)
		return
	}
	u, team, err := p.auth.CreateUser(body.Email, body.Name, body.Password)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := p.setSession(w, u.ID, team.ID); err != nil {
		http.Error(w, "could not create session", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (p *Panel) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ip := clientIP(r)
	if loginLimiter.blocked(ip, time.Now()) {
		http.Error(w, "too many login attempts — try again later", http.StatusTooManyRequests)
		return
	}
	var body struct{ Email, Password string }
	_ = json.NewDecoder(r.Body).Decode(&body)
	u, ok := p.auth.Authenticate(body.Email, body.Password)
	if !ok {
		loginLimiter.fail(ip, time.Now())
		http.Error(w, "wrong email or password", http.StatusUnauthorized)
		return
	}
	loginLimiter.ok(ip)
	teamID := ""
	if teams := p.auth.TeamsForUser(u.ID); len(teams) > 0 {
		teamID = teams[0].ID
	}
	p.finishSignIn(w, u.ID, teamID)
}

func (p *Panel) handleLogout(w http.ResponseWriter, r *http.Request) {
	if tok, ok := p.sessionToken(r); ok {
		deleteSession(tok)
	}
	p.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (p *Panel) handleAccept(w http.ResponseWriter, r *http.Request) {
	var body struct{ Token, Name, Password string }
	_ = json.NewDecoder(r.Body).Decode(&body)
	inv, ok := p.auth.Invite(body.Token)
	if !ok {
		http.Error(w, "invite not found or already used", http.StatusBadRequest)
		return
	}
	var user auth.User
	if existing, found := p.auth.UserByEmail(inv.Email); found {
		// Throttle like login — this verifies an existing account's password, else it's a brute-force oracle for any invite holder.
		ip := clientIP(r)
		if loginLimiter.blocked(ip, time.Now()) {
			http.Error(w, "too many attempts — try again later", http.StatusTooManyRequests)
			return
		}
		u, valid := p.auth.Authenticate(inv.Email, body.Password)
		if !valid {
			loginLimiter.fail(ip, time.Now())
			http.Error(w, "wrong password for "+existing.Email, http.StatusUnauthorized)
			return
		}
		loginLimiter.ok(ip)
		user = u
	} else {
		u, err := p.auth.CreateUserNoTeam(inv.Email, body.Name, body.Password)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		user = u
	}
	team, err := p.auth.AcceptInvite(body.Token, user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p.finishSignIn(w, user.ID, team.ID)
}

func (p *Panel) handleTeamSwitch(w http.ResponseWriter, r *http.Request) {
	var body struct{ Team string }
	_ = json.NewDecoder(r.Body).Decode(&body)
	s, _ := p.session(r)
	if _, ok := p.auth.Role(s.userID, body.Team); !ok {
		http.Error(w, "not a member of that team", http.StatusForbidden)
		return
	}
	if tok, ok := p.sessionToken(r); ok {
		setSessionTeam(tok, body.Team)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (p *Panel) handleTeamCreate(w http.ResponseWriter, r *http.Request) {
	var body struct{ Name string }
	_ = json.NewDecoder(r.Body).Decode(&body)
	if strings.TrimSpace(body.Name) == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	s, _ := p.session(r)
	team, err := p.auth.CreateTeam(strings.TrimSpace(body.Name), s.userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(team)
}

func (p *Panel) handleMembers(w http.ResponseWriter, r *http.Request) {
	s, _ := p.session(r)
	role, ok := p.auth.Role(s.userID, s.teamID)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodDelete:
		if role != auth.RoleOwner {
			http.Error(w, "only owners can remove members", http.StatusForbidden)
			return
		}
		uid := r.URL.Query().Get("user")
		if uid == s.userID {
			http.Error(w, "you can't remove yourself", http.StatusBadRequest)
			return
		}
		who := uid
		if u, ok := p.auth.User(uid); ok {
			who = u.Email
		}
		if err := p.auth.RemoveMember(s.teamID, uid); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		p.audit(r, "member.remove", who, "")
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(p.auth.Members(s.teamID))
	}
}

func (p *Panel) handleInvite(w http.ResponseWriter, r *http.Request) {
	var body struct{ Email, Role string }
	_ = json.NewDecoder(r.Body).Decode(&body)
	s, _ := p.session(r)
	if role, ok := p.auth.Role(s.userID, s.teamID); !ok || role != auth.RoleOwner {
		http.Error(w, "only owners can invite", http.StatusForbidden)
		return
	}
	inv, err := p.auth.CreateInvite(body.Email, s.teamID, body.Role)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	p.audit(r, "member.invite", strings.TrimSpace(body.Email), body.Role)
	link := baseURL(r) + "/invite/" + inv.Token
	out := map[string]any{"link": link}
	if relay := mail.RelayFromEnv(); relay != nil && inv.Email != "" {
		err := p.emailInvite(r.Context(), relay, s, inv.Email, link)
		if err != nil {
			log.Printf("skiff: emailing invite: %v", err)
		}
		out["emailed"] = err == nil
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (p *Panel) emailInvite(ctx context.Context, relay *mail.Relay, s sess, to, link string) error {
	if _, err := netmail.ParseAddress(to); err != nil {
		return err
	}
	inviter, _ := p.auth.User(s.userID)
	var team string
	for _, t := range p.auth.TeamsForUser(s.userID) {
		if t.ID == s.teamID {
			team = t.Name
		}
	}
	who := cmp.Or(inviter.Name, inviter.Email)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return relay.Send(ctx, to, mail.Letter{
		Subject: who + " invited you to " + team + " on Skiff",
		Heading: "Join " + team + " on Skiff",
		Body: []string{
			who + " (" + inviter.Email + ") invited you to join the " + team + " team on Skiff, where the team deploys and runs its apps.",
		},
		Button: "Accept invite",
		URL:    link,
		Note:   "The invite works once and expires in 7 days. If the button doesn't work, paste this link into your browser: " + link,
		Reason: "If you weren't expecting this invite, you can ignore this email.",
	})
}
