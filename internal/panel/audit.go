package panel

import (
	"encoding/json"
	"net/http"
	"time"
)

type AuditEntry struct {
	ID      int64  `json:"id"`
	Actor   string `json:"actor"`
	Action  string `json:"action"`
	Target  string `json:"target"`
	Detail  string `json:"detail"`
	Created int64  `json:"created"`
}

func recordAudit(team string, e AuditEntry) {
	if team == "" || sqlDB == nil {
		return
	}
	_, _ = sqlDB.Exec(
		`INSERT INTO audit(team,actor,action,target,detail,created) VALUES(?,?,?,?,?,?)`,
		team, e.Actor, e.Action, e.Target, e.Detail, time.Now().Unix())
}

// deployActor names whoever started a deploy from the dashboard.
func (p *Panel) deployActor(r *http.Request) string {
	s, ok := p.session(r)
	if !ok {
		return ""
	}
	u, ok := p.auth.User(s.userID)
	if !ok {
		return ""
	}
	if u.Name != "" {
		return u.Name
	}
	return u.Email
}

func (p *Panel) audit(r *http.Request, action, target, detail string) {
	s, ok := p.session(r)
	if !ok {
		return
	}
	actor := "someone"
	if u, ok := p.auth.User(s.userID); ok {
		if actor = u.Email; actor == "" {
			actor = u.Name
		}
	}
	recordAudit(s.teamID, AuditEntry{Actor: actor, Action: action, Target: target, Detail: detail})
}

func teamAudit(team string, before int64, limit int) []AuditEntry {
	q := `SELECT id,actor,action,target,detail,created FROM audit WHERE team=?`
	args := []any{team}
	if before > 0 {
		q += ` AND id < ?`
		args = append(args, before)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := sqlDB.Query(q, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		if rows.Scan(&e.ID, &e.Actor, &e.Action, &e.Target, &e.Detail, &e.Created) == nil {
			out = append(out, e)
		}
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

func (p *Panel) handleAudit(w http.ResponseWriter, r *http.Request) {
	before, limit := pageBounds(r)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(teamAudit(p.teamID(r), before, limit))
}
