// Package auth is Skiff's accounts + teams layer: users belong to teams, and projects/env/deploys are scoped to a team. Backed by SQLite.
package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	RoleOwner  = "owner"
	RoleMember = "member"
)

type User struct {
	ID           string `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	PasswordHash string `json:"passwordHash,omitempty"`
	Created      int64  `json:"created"`
}

type Team struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Slug    string `json:"slug"`
	Created int64  `json:"created"`
}

type Invite struct {
	Token   string `json:"token"`
	Email   string `json:"email"`
	TeamID  string `json:"teamId"`
	Role    string `json:"role"`
	Created int64  `json:"created"`
}

type Member struct {
	User User   `json:"user"`
	Role string `json:"role"`
}

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) HasUsers() bool {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return true // fail closed: a DB error must not re-open the first-run setup path
	}
	return n > 0
}

func (s *Store) CreateUser(email, name, password string) (User, Team, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || len(password) < 8 {
		return User{}, Team{}, fmt.Errorf("email and an 8+ character password are required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, Team{}, err
	}
	if name == "" {
		name = strings.SplitN(email, "@", 2)[0]
	}
	u := User{ID: id(), Email: email, Name: name, PasswordHash: string(hash), Created: now()}
	team := Team{ID: id(), Name: name + "'s team", Slug: slug(name), Created: now()}

	tx, err := s.db.Begin()
	if err != nil {
		return User{}, Team{}, err
	}
	defer tx.Rollback() //nolint:errcheck
	var exists int
	_ = tx.QueryRow(`SELECT COUNT(*) FROM users WHERE email = ?`, email).Scan(&exists)
	if exists > 0 {
		return User{}, Team{}, fmt.Errorf("an account with that email already exists")
	}
	if _, err := tx.Exec(`INSERT INTO users(id,email,name,password_hash,created) VALUES(?,?,?,?,?)`,
		u.ID, u.Email, u.Name, u.PasswordHash, u.Created); err != nil {
		return User{}, Team{}, err
	}
	if _, err := tx.Exec(`INSERT INTO teams(id,name,slug,created) VALUES(?,?,?,?)`,
		team.ID, team.Name, team.Slug, team.Created); err != nil {
		return User{}, Team{}, err
	}
	if _, err := tx.Exec(`INSERT INTO memberships(user_id,team_id,role) VALUES(?,?,?)`,
		u.ID, team.ID, RoleOwner); err != nil {
		return User{}, Team{}, err
	}
	return u, team, tx.Commit()
}

// CreateUserNoTeam adds a user without a personal team, for the invite flow where they join the inviter's team instead.
func (s *Store) CreateUserNoTeam(email, name, password string) (User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || len(password) < 8 {
		return User{}, fmt.Errorf("email and an 8+ character password are required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	if name == "" {
		name = strings.SplitN(email, "@", 2)[0]
	}
	u := User{ID: id(), Email: email, Name: name, PasswordHash: string(hash), Created: now()}
	if _, err := s.db.Exec(`INSERT INTO users(id,email,name,password_hash,created) VALUES(?,?,?,?,?)`,
		u.ID, u.Email, u.Name, u.PasswordHash, u.Created); err != nil {
		return User{}, fmt.Errorf("an account with that email already exists")
	}
	return u, nil
}

func (s *Store) Authenticate(email, password string) (User, bool) {
	u, ok := s.UserByEmail(email)
	if !ok {
		return User{}, false
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return User{}, false
	}
	return u, true
}

func (s *Store) scanUser(row *sql.Row) (User, bool) {
	var u User
	if err := row.Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Created); err != nil {
		return User{}, false
	}
	return u, true
}

func (s *Store) User(userID string) (User, bool) {
	return s.scanUser(s.db.QueryRow(
		`SELECT id,email,name,password_hash,created FROM users WHERE id = ?`, userID))
}

func (s *Store) UserByEmail(email string) (User, bool) {
	email = strings.ToLower(strings.TrimSpace(email))
	return s.scanUser(s.db.QueryRow(
		`SELECT id,email,name,password_hash,created FROM users WHERE email = ?`, email))
}

func (s *Store) TeamsForUser(userID string) []Team {
	rows, err := s.db.Query(`
		SELECT t.id,t.name,t.slug,t.created FROM teams t
		JOIN memberships m ON m.team_id = t.id
		WHERE m.user_id = ? ORDER BY t.created`, userID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Team
	for rows.Next() {
		var t Team
		if rows.Scan(&t.ID, &t.Name, &t.Slug, &t.Created) == nil {
			out = append(out, t)
		}
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

func (s *Store) Role(userID, teamID string) (string, bool) {
	var role string
	err := s.db.QueryRow(`SELECT role FROM memberships WHERE user_id = ? AND team_id = ?`,
		userID, teamID).Scan(&role)
	if err != nil {
		return "", false
	}
	return role, true
}

func (s *Store) CreateTeam(name, ownerID string) (Team, error) {
	t := Team{ID: id(), Name: name, Slug: slug(name), Created: now()}
	tx, err := s.db.Begin()
	if err != nil {
		return Team{}, err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(`INSERT INTO teams(id,name,slug,created) VALUES(?,?,?,?)`,
		t.ID, t.Name, t.Slug, t.Created); err != nil {
		return Team{}, err
	}
	if _, err := tx.Exec(`INSERT INTO memberships(user_id,team_id,role) VALUES(?,?,?)`,
		ownerID, t.ID, RoleOwner); err != nil {
		return Team{}, err
	}
	return t, tx.Commit()
}

func (s *Store) Members(teamID string) []Member {
	rows, err := s.db.Query(`
		SELECT u.id,u.email,u.name,u.created,m.role FROM users u
		JOIN memberships m ON m.user_id = u.id
		WHERE m.team_id = ? ORDER BY m.role, u.created`, teamID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var m Member
		if rows.Scan(&m.User.ID, &m.User.Email, &m.User.Name, &m.User.Created, &m.Role) == nil {
			out = append(out, m)
		}
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

func (s *Store) CreateInvite(email, teamID, role string) (Invite, error) {
	if role != RoleOwner {
		role = RoleMember
	}
	inv := Invite{Token: id() + id(), Email: strings.ToLower(strings.TrimSpace(email)),
		TeamID: teamID, Role: role, Created: now()}
	_, err := s.db.Exec(`INSERT INTO invites(token,email,team_id,role,created) VALUES(?,?,?,?,?)`,
		inv.Token, inv.Email, inv.TeamID, inv.Role, inv.Created)
	return inv, err
}

func (s *Store) Invite(token string) (Invite, bool) {
	var inv Invite
	err := s.db.QueryRow(`SELECT token,email,team_id,role,created FROM invites WHERE token = ?`, token).
		Scan(&inv.Token, &inv.Email, &inv.TeamID, &inv.Role, &inv.Created)
	if err != nil {
		return Invite{}, false
	}
	return inv, true
}

func (s *Store) AcceptInvite(token, userID string) (Team, error) {
	inv, ok := s.Invite(token)
	if !ok {
		return Team{}, fmt.Errorf("invite not found or already used")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Team{}, err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(
		`INSERT OR IGNORE INTO memberships(user_id,team_id,role) VALUES(?,?,?)`,
		userID, inv.TeamID, inv.Role); err != nil {
		return Team{}, err
	}
	if _, err := tx.Exec(`DELETE FROM invites WHERE token = ?`, token); err != nil {
		return Team{}, err
	}
	var t Team
	_ = tx.QueryRow(`SELECT id,name,slug,created FROM teams WHERE id = ?`, inv.TeamID).
		Scan(&t.ID, &t.Name, &t.Slug, &t.Created)
	return t, tx.Commit()
}

func (s *Store) UpdateName(userID, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("name is required")
	}
	_, err := s.db.Exec(`UPDATE users SET name = ? WHERE id = ?`, name, userID)
	return err
}

func (s *Store) ChangePassword(userID, current, next string) error {
	if len(next) < 8 {
		return fmt.Errorf("new password must be at least 8 characters")
	}
	u, ok := s.User(userID)
	if !ok {
		return fmt.Errorf("user not found")
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(current)) != nil {
		return fmt.Errorf("current password is incorrect")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(next), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, string(hash), userID)
	return err
}

func (s *Store) RenameTeam(teamID, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("name is required")
	}
	_, err := s.db.Exec(`UPDATE teams SET name = ?, slug = ? WHERE id = ?`, name, slug(name), teamID)
	return err
}

// RemoveMember drops a user from a team, refusing to remove the last owner.
func (s *Store) RemoveMember(teamID, userID string) error {
	role, ok := s.Role(userID, teamID)
	if !ok {
		return fmt.Errorf("not a member of this team")
	}
	if role == RoleOwner {
		var owners int
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM memberships WHERE team_id = ? AND role = ?`,
			teamID, RoleOwner).Scan(&owners)
		if owners <= 1 {
			return fmt.Errorf("can't remove the last owner")
		}
	}
	_, err := s.db.Exec(`DELETE FROM memberships WHERE team_id = ? AND user_id = ?`, teamID, userID)
	return err
}

// LeaveTeam drops the caller's own membership, refusing if they're the last owner.
func (s *Store) LeaveTeam(userID, teamID string) error {
	role, ok := s.Role(userID, teamID)
	if !ok {
		return fmt.Errorf("you're not a member of this team")
	}
	if role == RoleOwner {
		var owners int
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM memberships WHERE team_id = ? AND role = ?`,
			teamID, RoleOwner).Scan(&owners)
		if owners <= 1 {
			return fmt.Errorf("you're the last owner — delete the team instead")
		}
	}
	_, err := s.db.Exec(`DELETE FROM memberships WHERE team_id = ? AND user_id = ?`, teamID, userID)
	return err
}

// DeleteTeam removes a team with its memberships and invites; callers must first ensure it owns no apps or databases.
func (s *Store) DeleteTeam(teamID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(`DELETE FROM memberships WHERE team_id = ?`, teamID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM invites WHERE team_id = ?`, teamID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM teams WHERE id = ?`, teamID); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteUser removes a user and their memberships; callers must first deal with any teams the user solely owns.
func (s *Store) DeleteUser(userID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(`DELETE FROM memberships WHERE user_id = ?`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM recovery_codes WHERE user_id = ?`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM users WHERE id = ?`, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func id() string {
	b := make([]byte, 9)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func now() int64 { return time.Now().Unix() }

func slug(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('-')
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		s = "team"
	}
	return s
}
