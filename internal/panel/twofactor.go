package panel

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/nuelScript/skiff/internal/auth"
)

const (
	ticketTTL      = 5 * time.Minute
	ticketAttempts = 5
)

// signInTicket stands between a correct password and a session for accounts with two-factor on.
type signInTicket struct {
	userID   string
	teamID   string
	expires  time.Time
	attempts int
}

var (
	ticketMu sync.Mutex
	tickets  = map[string]*signInTicket{}
)

func issueTicket(userID, teamID string) string {
	ticketMu.Lock()
	defer ticketMu.Unlock()
	now := time.Now()
	for k, t := range tickets {
		if now.After(t.expires) {
			delete(tickets, k)
		}
	}
	tok := randToken()
	tickets[tok] = &signInTicket{userID: userID, teamID: teamID, expires: now.Add(ticketTTL)}
	return tok
}

// finishSignIn either opens a session or, when the account has two-factor on, answers with a ticket to trade for one with a code.
func (p *Panel) finishSignIn(w http.ResponseWriter, userID, teamID string) {
	if p.auth.HasTwoFactor(userID) {
		writeJSON(w, map[string]any{"twoFactor": true, "ticket": issueTicket(userID, teamID)})
		return
	}
	if err := p.setSession(w, userID, teamID); err != nil {
		http.Error(w, "could not create session", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (p *Panel) handleLoginCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ip := clientIP(r)
	if loginLimiter.blocked(ip, time.Now()) {
		http.Error(w, "too many attempts — try again later", http.StatusTooManyRequests)
		return
	}
	var body struct{ Ticket, Code string }
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body)

	ticketMu.Lock()
	t, ok := tickets[body.Ticket]
	if ok && (time.Now().After(t.expires) || t.attempts >= ticketAttempts) {
		delete(tickets, body.Ticket)
		ok = false
	}
	if ok {
		t.attempts++
	}
	ticketMu.Unlock()
	if !ok {
		http.Error(w, "sign-in expired — enter your password again", http.StatusUnauthorized)
		return
	}
	if !p.auth.CheckSecondFactor(t.userID, body.Code) {
		loginLimiter.fail(ip, time.Now())
		http.Error(w, auth.ErrBadCode.Error(), http.StatusUnauthorized)
		return
	}
	ticketMu.Lock()
	delete(tickets, body.Ticket)
	ticketMu.Unlock()
	loginLimiter.ok(ip)
	if err := p.setSession(w, t.userID, t.teamID); err != nil {
		http.Error(w, "could not create session", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleTwoFactor manages the signed-in user's own two-factor: GET status, POST setup / enable / disable / recovery.
func (p *Panel) handleTwoFactor(w http.ResponseWriter, r *http.Request) {
	s, ok := p.session(r)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	u, ok := p.auth.User(s.userID)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, map[string]any{"enabled": p.auth.HasTwoFactor(u.ID), "recoveryLeft": p.auth.RecoveryLeft(u.ID)})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct{ Action, Password, Code string }
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body)
	// Every change needs the password, so a session left open on someone else's machine can't switch it off or take the codes.
	ip := clientIP(r)
	if loginLimiter.blocked(ip, time.Now()) {
		http.Error(w, "too many attempts — try again later", http.StatusTooManyRequests)
		return
	}
	if body.Action != "enable" {
		if _, valid := p.auth.Authenticate(u.Email, body.Password); !valid {
			loginLimiter.fail(ip, time.Now())
			http.Error(w, "wrong password", http.StatusUnauthorized)
			return
		}
	}
	enabled := p.auth.HasTwoFactor(u.ID)
	switch body.Action {
	case "setup":
		if enabled {
			http.Error(w, "two-factor is already on", http.StatusConflict)
			return
		}
		secret, err := p.auth.BeginTwoFactor(u.ID)
		if err != nil {
			http.Error(w, "couldn't start setup", http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]string{"secret": secret, "uri": auth.TOTPURI(p.issuer(), u.Email, secret)})

	case "enable":
		codes, err := p.auth.EnableTwoFactor(u.ID, body.Code)
		if err != nil {
			loginLimiter.fail(ip, time.Now())
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// Other sessions were opened with the password alone; end them.
		if c, err := r.Cookie("skiff_session"); err == nil {
			_, _ = sqlDB.Exec(`DELETE FROM sessions WHERE user_id = ? AND token != ?`, u.ID, auth.StoredToken(c.Value))
		}
		p.audit(r, "account.2fa.enable", u.Email, "")
		writeJSON(w, map[string]any{"recoveryCodes": codes})

	case "disable", "recovery":
		if !enabled {
			http.Error(w, "two-factor is off", http.StatusConflict)
			return
		}
		if !p.auth.CheckSecondFactor(u.ID, body.Code) {
			loginLimiter.fail(ip, time.Now())
			http.Error(w, auth.ErrBadCode.Error(), http.StatusUnauthorized)
			return
		}
		if body.Action == "disable" {
			if err := p.auth.DisableTwoFactor(u.ID); err != nil {
				http.Error(w, "couldn't turn two-factor off", http.StatusInternalServerError)
				return
			}
			p.audit(r, "account.2fa.disable", u.Email, "")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		codes, err := p.auth.RegenerateRecovery(u.ID)
		if err != nil {
			http.Error(w, "couldn't make new codes", http.StatusInternalServerError)
			return
		}
		p.audit(r, "account.2fa.recovery", u.Email, "")
		writeJSON(w, map[string]any{"recoveryCodes": codes})

	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
	}
}

// issuer names the account in authenticator apps.
func (p *Panel) issuer() string {
	if p.domain == "" || p.domain == "localhost" {
		return "Skiff"
	}
	return "Skiff (" + p.domain + ")"
}
