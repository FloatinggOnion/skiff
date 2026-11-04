package panel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// API tokens authenticate the /api/v1 surface for CI; each is team-scoped, shown once at creation, and stored only as its SHA-256.

type APIToken struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Created  int64  `json:"created"`
	LastUsed int64  `json:"lastUsed"`
	// Token is set only in the create response — never persisted or listed.
	Token string `json:"token,omitempty"`
}

func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

func newAPIToken() string {
	return "skiff_" + randHex(24)
}

func createToken(team, name, createdBy string) (APIToken, error) {
	tok := newAPIToken()
	t := APIToken{ID: randToken()[:12], Name: name, Created: time.Now().Unix()}
	_, err := sqlDB.Exec(
		`INSERT INTO api_tokens(id,team,name,token_hash,created,last_used,created_by) VALUES(?,?,?,?,?,0,?)`,
		t.ID, team, name, hashToken(tok), t.Created, createdBy)
	if err != nil {
		return APIToken{}, err
	}
	t.Token = tok
	return t, nil
}

func listTokens(team string) []APIToken {
	rows, err := sqlDB.Query(
		`SELECT id,name,created,last_used FROM api_tokens WHERE team=? ORDER BY created DESC`, team)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []APIToken{}
	for rows.Next() {
		var t APIToken
		if rows.Scan(&t.ID, &t.Name, &t.Created, &t.LastUsed) == nil {
			out = append(out, t)
		}
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

func revokeToken(team, id string) {
	_, _ = sqlDB.Exec(`DELETE FROM api_tokens WHERE team=? AND id=?`, team, id)
}

func resolveToken(tok string) (team, name string, ok bool) {
	if tok == "" {
		return "", "", false
	}
	var id string
	err := sqlDB.QueryRow(
		`SELECT id,team,name FROM api_tokens WHERE token_hash=?`, hashToken(tok)).
		Scan(&id, &team, &name)
	if err != nil {
		return "", "", false
	}
	_, _ = sqlDB.Exec(`UPDATE api_tokens SET last_used=? WHERE id=?`, time.Now().Unix(), id)
	return team, name, true
}

func (p *Panel) handleTokens(w http.ResponseWriter, r *http.Request) {
	team := p.teamID(r)
	if team == "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, listTokens(team))
	case http.MethodPost:
		var body struct{ Name string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		name := strings.TrimSpace(body.Name)
		if name == "" {
			name = "token"
		}
		s, _ := p.session(r)
		t, err := createToken(team, name, s.userID)
		if err != nil {
			http.Error(w, "could not create token", http.StatusInternalServerError)
			return
		}
		p.audit(r, "token.create", name, "")
		writeJSON(w, t) // includes the plaintext token, this one time
	case http.MethodDelete:
		revokeToken(team, r.URL.Query().Get("id"))
		p.audit(r, "token.revoke", "", "")
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
