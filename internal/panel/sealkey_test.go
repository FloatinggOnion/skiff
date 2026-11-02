package panel

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

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
	sealTestKey(t)
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
