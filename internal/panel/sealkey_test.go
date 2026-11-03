package panel

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nuelScript/skiff/internal/auth"
	"github.com/nuelScript/skiff/internal/cpbackup"
	"github.com/nuelScript/skiff/internal/db"
	"github.com/nuelScript/skiff/internal/seal"
)

func sealTestKey(t *testing.T) {
	t.Helper()
	if err := seal.Init(bytes.Repeat([]byte{7}, 32)); err != nil {
		t.Fatal(err)
	}
}

func TestInitSealChecksTheKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(skiffDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	openTestDB(t)
	if err := initSeal(); err != nil {
		t.Fatalf("first start: %v", err)
	}
	if err := initSeal(); err != nil {
		t.Fatalf("second start: %v", err)
	}
	path := filepath.Join(skiffDir(), "recovery.key")

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := initSeal(); err == nil {
		t.Fatal("started without the key")
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("a lost key must not be replaced with a new one")
	}

	other := hex.EncodeToString(bytes.Repeat([]byte{9}, 32))
	if err := os.WriteFile(path, []byte(other), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := initSeal(); err == nil {
		t.Fatal("started with the wrong key")
	}
}

func TestSecretColumnsReadBothForms(t *testing.T) {
	openTestDB(t)
	s := func(v string) string {
		t.Helper()
		out, err := seal.Seal(v)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := sqlDB.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	for _, table := range []string{"env_vars", "preview_env"} {
		exec(`INSERT INTO `+table+`(app,key,value,build) VALUES('a','OLD','plain',0),('a','NEW',?,0)`, s("secret"))
	}
	exec(`INSERT INTO shared_env(team,key,value,build) VALUES('t','OLD','plain',0),('t','NEW',?,0)`, s("secret"))
	want := map[string]string{"OLD": "plain", "NEW": "secret"}
	for name, vars := range map[string][]EnvVar{"env": getEnv("a"), "preview": getPreviewEnv("a"), "shared": sharedEnv("t")} {
		if len(vars) != 2 {
			t.Fatalf("%s: %+v", name, vars)
		}
		for _, v := range vars {
			if want[v.Key] != v.Value {
				t.Fatalf("%s: %+v", name, v)
			}
		}
	}

	exec(`INSERT INTO databases(id,team,name,engine,container,host,port,username,password,dbname,created)
		VALUES('d','t','n','postgres','c','h',5432,'u',?,'db',1)`, s("dbpass"))
	if d, ok := getDB("d"); !ok || d.Password != "dbpass" {
		t.Fatalf("db: %+v", d)
	}
	exec(`INSERT INTO alerts(team,email,slack_url,webhook_url) VALUES('t','e',?,?)`, s("slack"), s("hook"))
	if a := getAlerts("t"); a.SlackURL != "slack" || a.WebhookURL != "hook" || a.Email != "e" {
		t.Fatalf("alerts: %+v", a)
	}
	exec(`INSERT INTO log_drains(team,url,header_name,header_value) VALUES('t',?,'Auth',?)`, s("https://d"), s("tok"))
	if d, ok := getLogDrain("t"); !ok || d.URL != "https://d" || d.HeaderValue != "tok" || d.HeaderName != "Auth" {
		t.Fatalf("drain: %+v", d)
	}
	exec(`INSERT INTO registry_creds(app,username,password) VALUES('a','u',?)`, s("regpass"))
	if u, p, ok := getRegistryCreds("a"); !ok || u != "u" || p != "regpass" {
		t.Fatalf("registry: %q %q", u, p)
	}
	exec(`INSERT INTO backup_targets(team,endpoint,bucket,access_key,secret_key,updated) VALUES('t','e','b',?,?,1)`, s("ak"), s("sk"))
	if o := getOffsite("t"); o.AccessKey != "ak" || o.SecretKey != "sk" {
		t.Fatalf("offsite: %+v", o)
	}
	exec(`INSERT INTO buckets(id,team,name,container,access_key,secret_key,created) VALUES('b','t','n','c',?,?,1)`, s("bak"), s("bsk"))
	if b, ok := getBucket("b"); !ok || b.AccessKey != "bak" || b.SecretKey != "bsk" {
		t.Fatalf("bucket: %+v", b)
	}
	if all := allBuckets(); len(all) != 1 || all[0].SecretKey != "bsk" {
		t.Fatalf("all buckets: %+v", all)
	}
}

func TestWritesAreSealed(t *testing.T) {
	openTestDB(t)
	if err := setEnv("a", []EnvVar{{Key: "K", Value: "v1"}}); err != nil {
		t.Fatal(err)
	}
	if err := setPreviewEnv("a", []EnvVar{{Key: "K", Value: "v2"}}); err != nil {
		t.Fatal(err)
	}
	if err := setSharedEnv("t", []EnvVar{{Key: "K", Value: "v3"}}); err != nil {
		t.Fatal(err)
	}
	if err := upsertEnvVar("b", "K", "v4", false); err != nil {
		t.Fatal(err)
	}
	if err := putDB(dbRow{ID: "d", Team: "t", Name: "n", Engine: "postgres", Container: "c", Host: "h", Password: "v5"}); err != nil {
		t.Fatal(err)
	}
	if err := setAlerts("t", AlertConfig{Email: "e", SlackURL: "v6", WebhookURL: "v7"}); err != nil {
		t.Fatal(err)
	}
	if err := saveLogDrain(LogDrain{Team: "t", URL: "v8", HeaderName: "H", HeaderValue: "v9"}); err != nil {
		t.Fatal(err)
	}
	if err := setRegistryCreds("a", "u", "v10"); err != nil {
		t.Fatal(err)
	}
	if err := setOffsite("t", OffsiteTarget{Endpoint: "e", Bucket: "b", AccessKey: "v11", SecretKey: "v12"}); err != nil {
		t.Fatal(err)
	}
	if err := putBucket(bucketRow{ID: "b", Team: "t", Name: "n", Container: "c", AccessKey: "v13", SecretKey: "v14", Created: 1}); err != nil {
		t.Fatal(err)
	}
	plain, err := findPlainSecrets()
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) != 0 {
		t.Fatalf("stored plain: %+v", plain)
	}
	if got := getEnv("a"); len(got) != 1 || got[0].Value != "v1" {
		t.Fatalf("env: %+v", got)
	}
	if d, ok := getDB("d"); !ok || d.Password != "v5" {
		t.Fatalf("db: %+v", d)
	}
	if o := getOffsite("t"); o.SecretKey != "v12" {
		t.Fatalf("offsite: %+v", o)
	}
	if d, ok := getLogDrain("t"); !ok || d.URL != "v8" || d.HeaderValue != "v9" {
		t.Fatalf("drain: %+v", d)
	}
}

func TestSealExistingLeavesNoPlainValuesOnDisk(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(skiffDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "skiff.db")
	database, err := db.OpenAt(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	sqlDB = database
	if err := initSeal(); err != nil {
		t.Fatal(err)
	}
	const secret = "correct-horse-battery-staple"
	for i, q := range []string{
		`INSERT INTO env_vars(app,key,value,build) VALUES('a','K',?,0)`,
		`INSERT INTO preview_env(app,key,value,build) VALUES('a','K',?,0)`,
		`INSERT INTO shared_env(team,key,value,build) VALUES('t','K',?,0)`,
		`INSERT INTO databases(id,team,name,engine,container,host,port,username,password,dbname,created) VALUES('d','t','n','e','c','h',1,'u',?,'db',1)`,
		`INSERT INTO alerts(team,email,slack_url,webhook_url) VALUES('t','','',?)`,
		`INSERT INTO log_drains(team,url,header_name,header_value) VALUES('t','https://d','H',?)`,
		`INSERT INTO registry_creds(app,username,password) VALUES('a','u',?)`,
		`INSERT INTO backup_targets(team,endpoint,bucket,access_key,secret_key,updated) VALUES('t','e','b','ak',?,1)`,
		`INSERT INTO buckets(id,team,name,container,access_key,secret_key,created) VALUES('b','t','n','c','ak',?,1)`,
		`INSERT INTO users(id,email,password_hash,created,totp_secret) VALUES('u','e','h',1,?)`,
	} {
		if _, err := sqlDB.Exec(q, secret); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	if err := sealExisting(); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{path, path + "-wal"} {
		raw, err := os.ReadFile(f)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), secret) {
			t.Fatalf("%s still holds the plain value", f)
		}
	}
	if got := getEnv("a"); len(got) != 1 || got[0].Value != secret {
		t.Fatalf("env after sealing: %+v", got)
	}
	var totp, step string
	if err := sqlDB.QueryRow(`SELECT totp_secret, totp_step FROM users WHERE id='u'`).Scan(&totp, &step); err != nil {
		t.Fatal(err)
	}
	if !seal.IsSealed(totp) {
		t.Fatalf("2FA secret not sealed: %q", totp)
	}

	files, _ := filepath.Glob(filepath.Join(skiffDir(), preEncryptionGlob))
	if len(files) != 1 {
		t.Fatalf("expected one pre-encryption backup, got %v", files)
	}
	sealed, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sealed), secret) {
		t.Fatal("the backup holds the plain value")
	}
	k, err := recoveryKey()
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := cpbackup.Open(k, sealed)
	if err != nil {
		t.Fatalf("the backup can't be opened: %v", err)
	}
	dest := t.TempDir()
	if _, err := cpbackup.Unpack(bytes.NewReader(bundle), dest); err != nil {
		t.Fatalf("the backup can't be unpacked: %v", err)
	}
	restored, err := db.OpenAt(filepath.Join(dest, cpbackup.DBName))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var was string
	if err := restored.QueryRow(`SELECT value FROM env_vars WHERE app='a'`).Scan(&was); err != nil || was != secret {
		t.Fatalf("backup env value: %q %v", was, err)
	}

	if err := sealExisting(); err != nil {
		t.Fatal(err)
	}
	if again, _ := filepath.Glob(filepath.Join(skiffDir(), preEncryptionGlob)); len(again) != 1 {
		t.Fatalf("a second start must not back up again: %v", again)
	}
	removeOldPreEncryptionBackups(time.Now().Add(preEncryptionKeep + time.Hour))
	if left, _ := filepath.Glob(filepath.Join(skiffDir(), preEncryptionGlob)); len(left) != 0 {
		t.Fatalf("old backup kept: %v", left)
	}
}

func TestInterruptedScrubRunsAgain(t *testing.T) {
	openTestDB(t)
	if _, err := sqlDB.Exec(`INSERT INTO instance_settings(key,value) VALUES(?,'1')`, sealScrubKey); err != nil {
		t.Fatal(err)
	}
	if err := sealExisting(); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM instance_settings WHERE key=?`, sealScrubKey).Scan(&n); err != nil || n != 0 {
		t.Fatalf("flag left set: %d %v", n, err)
	}
}

func TestNewSessionsAreHashed(t *testing.T) {
	openTestDB(t)
	const tok = "cookie-value"
	if err := putSession(tok, "u", "t"); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := sqlDB.QueryRow(`SELECT token FROM sessions`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == tok || !auth.IsStoredToken(stored) {
		t.Fatalf("stored as %q", stored)
	}
	if _, ok := getSession(stored); ok {
		t.Fatal("the stored value must not work as a cookie")
	}
	setSessionTeam(tok, "t2")
	if s, ok := getSession(tok); !ok || s.teamID != "t2" {
		t.Fatalf("session: %+v %v", s, ok)
	}
	deleteSession(tok)
	if _, ok := getSession(tok); ok {
		t.Fatal("deleted session still works")
	}
}

func TestExistingTokensAreHashedAndKeepWorking(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(skiffDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "skiff.db")
	database, err := db.OpenAt(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	sqlDB = database
	if err := initSeal(); err != nil {
		t.Fatal(err)
	}
	const sessTok, invTok = "old-session-token-abcdef", "old-invite-token-abcdef"
	if _, err := sqlDB.Exec(`INSERT INTO sessions(token,user_id,team_id,created) VALUES(?,'u','t',?)`, sessTok, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO invites(token,email,team_id,role,created) VALUES(?,'e','t','member',1)`, invTok); err != nil {
		t.Fatal(err)
	}
	if err := hashStoredTokens(); err != nil {
		t.Fatal(err)
	}
	if err := sealExisting(); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{path, path + "-wal"} {
		raw, err := os.ReadFile(f)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), sessTok) || strings.Contains(string(raw), invTok) {
			t.Fatalf("%s still holds a full token", f)
		}
	}
	if _, ok := getSession(sessTok); !ok {
		t.Fatal("signed-in user was logged out")
	}
	store := auth.NewStore(sqlDB)
	if inv, ok := store.Invite(invTok); !ok || inv.Token != invTok {
		t.Fatalf("open invite stopped working: %+v", inv)
	}
	if err := hashStoredTokens(); err != nil {
		t.Fatal(err)
	}
	if _, ok := getSession(sessTok); !ok {
		t.Fatal("a second start must not hash twice")
	}
}
