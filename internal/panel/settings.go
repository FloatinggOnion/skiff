package panel

import (
	"encoding/json"
	"net/http"

	"github.com/nuelScript/skiff/internal/auth"
)

func (p *Panel) handleAccount(w http.ResponseWriter, r *http.Request) {
	s, ok := p.session(r)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct{ Name string }
	_ = json.NewDecoder(r.Body).Decode(&body)
	if err := p.auth.UpdateName(s.userID, body.Name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (p *Panel) handlePassword(w http.ResponseWriter, r *http.Request) {
	s, ok := p.session(r)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct{ Current, Password string }
	_ = json.NewDecoder(r.Body).Decode(&body)
	if err := p.auth.ChangePassword(s.userID, body.Current, body.Password); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func countTeamApps(team string) int {
	var n int
	_ = sqlDB.QueryRow(`SELECT COUNT(*) FROM sources WHERE team = ?`, team).Scan(&n)
	return n
}

func countTeamDatabases(team string) int {
	var n int
	_ = sqlDB.QueryRow(`SELECT COUNT(*) FROM databases WHERE team = ?`, team).Scan(&n)
	return n
}

func (p *Panel) reassignTeam(w http.ResponseWriter, r *http.Request, userID string) {
	next := ""
	if teams := p.auth.TeamsForUser(userID); len(teams) > 0 {
		next = teams[0].ID
	}
	if c, err := r.Cookie("skiff_session"); err == nil {
		setSessionTeam(c.Value, next)
	}
}

func (p *Panel) handleAccountDelete(w http.ResponseWriter, r *http.Request) {
	s, ok := p.session(r)
	if !ok || r.Method != http.MethodPost {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var body struct{ Password string }
	_ = json.NewDecoder(r.Body).Decode(&body)
	u, ok := p.auth.User(s.userID)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if _, valid := p.auth.Authenticate(u.Email, body.Password); !valid {
		http.Error(w, "wrong password", http.StatusUnauthorized)
		return
	}

	var soloTeams []string
	for _, t := range p.auth.TeamsForUser(s.userID) {
		members := p.auth.Members(t.ID)
		if len(members) <= 1 {
			if countTeamApps(t.ID) > 0 || countTeamDatabases(t.ID) > 0 {
				http.Error(w, "Remove the projects and databases in “"+t.Name+"” before deleting your account.", http.StatusBadRequest)
				return
			}
			soloTeams = append(soloTeams, t.ID)
			continue
		}
		owners := 0
		for _, m := range members {
			if m.Role == auth.RoleOwner {
				owners++
			}
		}
		if role, _ := p.auth.Role(s.userID, t.ID); role == auth.RoleOwner && owners <= 1 {
			http.Error(w, "You're the last owner of “"+t.Name+"”. Hand it over or delete that team first.", http.StatusBadRequest)
			return
		}
	}

	for _, tid := range soloTeams {
		_, _ = sqlDB.Exec(`DELETE FROM shared_env WHERE team = ?`, tid)
		deleteLogDrain(tid)
		if p.auth.DeleteTeam(tid) == nil {
			deleteTeamMail(tid)
		}
	}
	if err := p.auth.DeleteUser(s.userID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = sqlDB.Exec(`DELETE FROM sessions WHERE user_id = ?`, s.userID)
	http.SetCookie(w, &http.Cookie{Name: "skiff_session", Value: "", Path: "/", MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

func (p *Panel) handleTeamLeave(w http.ResponseWriter, r *http.Request) {
	s, ok := p.session(r)
	if !ok || r.Method != http.MethodPost {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if len(p.auth.TeamsForUser(s.userID)) <= 1 {
		http.Error(w, "you can't leave your only team", http.StatusBadRequest)
		return
	}
	if err := p.auth.LeaveTeam(s.userID, s.teamID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p.reassignTeam(w, r, s.userID)
	w.WriteHeader(http.StatusNoContent)
}

func (p *Panel) handleTeamDelete(w http.ResponseWriter, r *http.Request) {
	s, ok := p.session(r)
	if !ok || r.Method != http.MethodPost {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if role, ok := p.auth.Role(s.userID, s.teamID); !ok || role != auth.RoleOwner {
		http.Error(w, "only owners can delete a team", http.StatusForbidden)
		return
	}
	if len(p.auth.TeamsForUser(s.userID)) <= 1 {
		http.Error(w, "you can't delete your only team — create another first, or delete your account", http.StatusBadRequest)
		return
	}
	if countTeamApps(s.teamID) > 0 {
		http.Error(w, "remove this team's projects first", http.StatusBadRequest)
		return
	}
	if countTeamDatabases(s.teamID) > 0 {
		http.Error(w, "remove this team's databases first", http.StatusBadRequest)
		return
	}
	_, _ = sqlDB.Exec(`DELETE FROM shared_env WHERE team = ?`, s.teamID)
	deleteLogDrain(s.teamID)
	if err := p.auth.DeleteTeam(s.teamID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	deleteTeamMail(s.teamID)
	p.reassignTeam(w, r, s.userID)
	w.WriteHeader(http.StatusNoContent)
}

func (p *Panel) handleTeamRename(w http.ResponseWriter, r *http.Request) {
	s, ok := p.session(r)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if role, ok := p.auth.Role(s.userID, s.teamID); !ok || role != auth.RoleOwner {
		http.Error(w, "only owners can rename the team", http.StatusForbidden)
		return
	}
	var body struct{ Name string }
	_ = json.NewDecoder(r.Body).Decode(&body)
	if err := p.auth.RenameTeam(s.teamID, body.Name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
