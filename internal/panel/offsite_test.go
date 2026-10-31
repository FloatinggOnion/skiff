package panel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOffsiteFromBodyKeepsStoredSecret(t *testing.T) {
	r := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(
		`{"endpoint":"https://s3.example.com/","bucket":"b","accessKey":"ak","prefix":"/skiff/"}`))
	got, err := offsiteFromBody(r, OffsiteTarget{SecretKey: "stored"})
	if err != nil {
		t.Fatal(err)
	}
	if got.SecretKey != "stored" || got.Endpoint != "https://s3.example.com" || got.Prefix != "skiff" {
		t.Fatalf("got %+v", got)
	}
	r = httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"endpoint":"ftp://x","bucket":"b","accessKey":"a","secretKey":"s"}`))
	if _, err := offsiteFromBody(r, OffsiteTarget{}); err == nil {
		t.Fatal("accepted a non-http endpoint")
	}
}

func TestOffsiteKey(t *testing.T) {
	d := dbRow{ID: "abc", Name: "main"}
	b := backupRow{File: "x1.sql", Created: 1793418402}
	if got := offsiteKey("skiff", d, b); got != "skiff/main-abc/20261031T034642Z-x1.sql" {
		t.Fatalf("got %q", got)
	}
	if got := offsiteKey("", d, b); !strings.HasPrefix(got, "main-abc/") {
		t.Fatalf("got %q", got)
	}
}

func TestOffsiteRefusesPrivateEndpoints(t *testing.T) {
	c := OffsiteTarget{Endpoint: "http://127.0.0.1:1", Bucket: "b", AccessKey: "a", SecretKey: "s"}.client()
	err := c.Check(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "non-public") {
		t.Fatalf("got %v", err)
	}
}

func TestUploadOffsiteLive(t *testing.T) {
	ep := os.Getenv("S3_PROBE_ENDPOINT")
	if ep == "" {
		t.Skip("no probe endpoint")
	}
	openTestDB(t)
	t.Setenv("HOME", t.TempDir())
	prev := offsiteHTTP
	offsiteHTTP = http.DefaultClient
	defer func() { offsiteHTTP = prev }()

	if err := setOffsite("t1", OffsiteTarget{Endpoint: ep, Bucket: "offsite", Prefix: "ci", AccessKey: "probeuser", SecretKey: "probepass123"}); err != nil {
		t.Fatal(err)
	}
	d := dbRow{ID: "db1", Team: "t1", Name: "main", Engine: "postgres"}
	for i, id := range []string{"b1", "b2"} {
		b := backupRow{ID: id, DbID: "db1", Team: "t1", Engine: "postgres", File: id + ".sql", Created: int64(1793418000 + i)}
		if err := os.MkdirAll(filepath.Dir(backupPath(b)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(backupPath(b), []byte("dump "+id), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := putBackup(b); err != nil {
			t.Fatal(err)
		}
		if err := uploadOffsite(d, b); err != nil {
			t.Fatal(err)
		}
		if got, _ := getBackup(id); got.Offsite != "ok" {
			t.Fatalf("backup %s offsite = %q", id, got.Offsite)
		}
	}
	pruneOffsite(getOffsite("t1").client(), "db1", 1)
	var n int
	_ = sqlDB.QueryRow(`SELECT COUNT(*) FROM offsite_objects WHERE db_id='db1'`).Scan(&n)
	if n != 1 {
		t.Fatalf("%d offsite objects tracked after pruning to 1", n)
	}
}
