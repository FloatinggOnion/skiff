package panel

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRemoveDatabaseBackups(t *testing.T) {
	openTestDB(t)
	t.Setenv("HOME", t.TempDir())
	for _, b := range []backupRow{
		{ID: "a", DbID: "gone", Team: "t", Engine: "postgres", File: "a.sql", Created: 1},
		{ID: "b", DbID: "gone", Team: "t", Engine: "postgres", File: "b.sql", Created: 2},
		{ID: "c", DbID: "kept", Team: "t", Engine: "postgres", File: "c.sql", Created: 3},
	} {
		if err := os.MkdirAll(filepath.Dir(backupPath(b)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(backupPath(b), []byte("dump"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := putBackup(b); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := sqlDB.Exec(`INSERT INTO offsite_objects(db_id,key,created) VALUES('gone','k',1)`); err != nil {
		t.Fatal(err)
	}
	removeDatabaseBackups("gone")
	if got := listBackupRows("gone"); len(got) != 0 {
		t.Fatalf("rows left: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(backupsDir(), "gone")); !os.IsNotExist(err) {
		t.Fatalf("files left: %v", err)
	}
	if got := listBackupRows("kept"); len(got) != 1 {
		t.Fatalf("another database's backups were touched: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(backupsDir(), "kept", "c.sql")); err != nil {
		t.Fatalf("another database's file was touched: %v", err)
	}
}

func TestRemoveUnlistedBackups(t *testing.T) {
	openTestDB(t)
	t.Setenv("HOME", t.TempDir())
	if _, err := sqlDB.Exec(`INSERT INTO databases(id,team,name,engine,container,host,port,created) VALUES('db1','t','n','postgres','c','h',5432,1)`); err != nil {
		t.Fatal(err)
	}
	listed := backupRow{ID: "a", DbID: "db1", Team: "t", Engine: "postgres", File: "listed.sql", Created: 1}
	if err := putBackup(listed); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(backupsDir(), "db1")
	if err := os.MkdirAll(filepath.Join(dir, "wal"), 0o700); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * unlistedBackupAge)
	for _, name := range []string{"listed.sql", "partial.sql", "fresh.sql"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if name != "fresh.sql" {
			if err := os.Chtimes(path, old, old); err != nil {
				t.Fatal(err)
			}
		}
	}
	removeUnlistedBackups(time.Now())
	for name, want := range map[string]bool{"listed.sql": true, "partial.sql": false, "fresh.sql": true, "wal": true} {
		if _, err := os.Stat(filepath.Join(dir, name)); (err == nil) != want {
			t.Errorf("%s: want present=%v, err %v", name, want, err)
		}
	}
}
