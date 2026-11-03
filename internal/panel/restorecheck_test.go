package panel

import (
	"testing"
	"time"
)

func TestNextRestoreCheck(t *testing.T) {
	now := time.Now().Unix()
	old := now - 25*3600
	recent := now - 3600
	cases := []struct {
		name string
		rows []backupRow
		want string
	}{
		{"no backups", nil, ""},
		{"never checked", []backupRow{{ID: "new"}}, "new"},
		{"newest checked long ago stays done", []backupRow{{ID: "new", CheckStatus: checkPassed, CheckAt: old}}, ""},
		{"a new backup is due", []backupRow{{ID: "new"}, {ID: "was", CheckStatus: checkPassed, CheckAt: old}}, "new"},
		{"a new backup waits for the day", []backupRow{{ID: "new"}, {ID: "was", CheckStatus: checkPassed, CheckAt: recent}}, ""},
		{"a failure isn't retried on the same file", []backupRow{{ID: "new", CheckStatus: checkFailed, CheckAt: old}}, ""},
		{"a failure gets a new backup checked", []backupRow{{ID: "new"}, {ID: "was", CheckStatus: checkFailed, CheckAt: old}}, "new"},
		{"a skipped check is tried the next day", []backupRow{{ID: "new", CheckStatus: checkSkipped, CheckAt: old}}, "new"},
		{"a skipped check waits for the day", []backupRow{{ID: "new", CheckStatus: checkSkipped, CheckAt: recent}}, ""},
		{"a running check holds the next one", []backupRow{{ID: "new"}, {ID: "was", CheckStatus: checkRunning, CheckAt: recent}}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, due := nextRestoreCheck(c.rows, now)
			if due != (c.want != "") || b.ID != c.want {
				t.Fatalf("got %q due=%v, want %q", b.ID, due, c.want)
			}
		})
	}
}

func TestRestoreVerdict(t *testing.T) {
	e := dbEngines["postgres"]
	for _, c := range []struct {
		restored, live int
		status, detail string
	}{
		{12, 12, checkPassed, "Restored 12 tables."},
		{1, 40, checkPassed, "Restored 1 table."},
		{0, 0, checkPassed, "Restored 0 tables."},
		{0, 3, checkFailed, "The backup restored, but it's empty and the database has 3 tables."},
	} {
		got := restoreVerdict(e, c.restored, c.live)
		if got.Status != c.status || got.Detail != c.detail {
			t.Errorf("%d of %d: got %+v", c.restored, c.live, got)
		}
	}
}

func TestParseItemCount(t *testing.T) {
	for out, want := range map[string]int{
		" 12\n": 12,
		"# Keyspace\r\ndb0:keys=3,expires=0,avg_ttl=0\r\ndb2:keys=4,expires=1,avg_ttl=9\r\n": 7,
		"# Keyspace\r\n": 0,
	} {
		if got, err := parseItemCount(out); err != nil || got != want {
			t.Errorf("%q: got %d %v, want %d", out, got, err, want)
		}
	}
	if _, err := parseItemCount("not a number"); err == nil {
		t.Error("nonsense output counted")
	}
}

func TestRestoreCheckSummary(t *testing.T) {
	openTestDB(t)
	add := func(id, status string, at int64) {
		if err := putBackup(backupRow{ID: id, DbID: "db", Team: "t", Engine: "postgres", File: id, Created: at, CheckStatus: status, CheckAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	if at, failed := restoreCheckSummary("db"); at != 0 || failed {
		t.Fatalf("no checks: %d %v", at, failed)
	}
	add("a", checkPassed, 100)
	add("skip", checkSkipped, 150)
	if at, failed := restoreCheckSummary("db"); at != 100 || failed {
		t.Fatalf("a skip mustn't count: %d %v", at, failed)
	}
	add("b", checkFailed, 200)
	if at, failed := restoreCheckSummary("db"); at != 100 || !failed {
		t.Fatalf("after a failure: %d %v", at, failed)
	}
	add("c", checkPassed, 300)
	if at, failed := restoreCheckSummary("db"); at != 300 || failed {
		t.Fatalf("after recovering: %d %v", at, failed)
	}
}
