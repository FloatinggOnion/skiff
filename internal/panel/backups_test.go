package panel

import (
	"os"
	"path/filepath"
	"testing"
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
