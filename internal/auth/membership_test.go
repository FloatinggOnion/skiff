package auth

import (
	"testing"
	"time"
)

func TestAuthenticateUnknownEmailStillHashes(t *testing.T) {
	s := newTestStore(t)
	start := time.Now()
	if _, ok := s.Authenticate("nobody@example.com", "whatever-pass"); ok {
		t.Fatal("unknown email authenticated")
	}
	if took := time.Since(start); took < 10*time.Millisecond {
		t.Fatalf("unknown email returned in %v, want a bcrypt-sized wait", took)
	}
}

func TestInviteExpires(t *testing.T) {
	s := newTestStore(t)
	_, team, _ := s.CreateUser("owner@example.com", "Owner", "password123")
	inv, err := s.CreateInvite("new@example.com", team.ID, RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Invite(inv.Token); !ok {
		t.Fatal("fresh invite refused")
	}
	old := time.Now().Add(-InviteTTL - time.Hour).Unix()
	if _, err := s.db.Exec(`UPDATE invites SET created = ?`, old); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Invite(inv.Token); ok {
		t.Fatal("expired invite accepted")
	}
	if _, err := s.AcceptInvite(inv.Token, "someone"); err == nil {
		t.Fatal("expired invite joined a team")
	}
}

func TestLeavingATeamEndsItsAccess(t *testing.T) {
	s := newTestStore(t)
	owner, team, _ := s.CreateUser("owner@example.com", "Owner", "password123")
	member, _, _ := s.CreateUser("member@example.com", "Member", "password123")
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(`INSERT INTO memberships(user_id,team_id,role) VALUES(?,?,?)`, member.ID, team.ID, RoleMember)
	mustExec(`INSERT INTO sessions(token,user_id,team_id,created) VALUES('s1',?,?,1)`, member.ID, team.ID)
	mustExec(`INSERT INTO api_tokens(id,team,name,token_hash,created,created_by) VALUES('t1',?,'ci','h1',1,?)`, team.ID, member.ID)
	mustExec(`INSERT INTO api_tokens(id,team,name,token_hash,created,created_by) VALUES('t2',?,'own','h2',1,?)`, team.ID, owner.ID)
	mustExec(`INSERT INTO api_tokens(id,team,name,token_hash,created) VALUES('t3',?,'old','h3',1)`, team.ID)

	if err := s.RemoveMember(team.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	var sessTeam string
	if err := s.db.QueryRow(`SELECT team_id FROM sessions WHERE token='s1'`).Scan(&sessTeam); err != nil || sessTeam != "" {
		t.Fatalf("session team = %q, %v", sessTeam, err)
	}
	var ids []string
	rows, _ := s.db.Query(`SELECT id FROM api_tokens ORDER BY id`)
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	if len(ids) != 2 || ids[0] != "t2" || ids[1] != "t3" {
		t.Fatalf("tokens left = %v, want only the owner's and the unattributed one", ids)
	}
}
