package panel

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestBaseFor(t *testing.T) {
	bases := []pitrBase{{ID: "a", Created: 100}, {ID: "b", Created: 200}, {ID: "c", Created: 300}}
	for at, want := range map[int64]string{99: "", 100: "a", 199: "a", 200: "b", 299: "b", 1000: "c"} {
		got, ok := baseFor(bases, at)
		if ok != (want != "") || got.ID != want {
			t.Errorf("at %d: got %q %v, want %q", at, got.ID, ok, want)
		}
	}
	if _, ok := baseFor(nil, 100); ok {
		t.Error("found a base in an empty list")
	}
}

func TestWithPITRArgs(t *testing.T) {
	if got := withPITRArgs(nil); got[0] != "postgres" || !slices.Contains(got, "archive_mode=on") {
		t.Fatalf("a plain database: %v", got)
	}
	tls := dbEngines["postgres"].tls.cmd
	before := slices.Clone(tls)
	got := withPITRArgs(tls)
	if !slices.Equal(got[:len(tls)], before) || !slices.Contains(got, "archive_mode=on") {
		t.Fatalf("a public database: %v", got)
	}
	if !slices.Equal(dbEngines["postgres"].tls.cmd, before) {
		t.Fatal("changed the engine's shared command")
	}
}

func TestArchiveCommandStopsWhenPaused(t *testing.T) {
	if !strings.HasPrefix(pitrArchiveCommand, "test -f "+pitrMount+"/PAUSED ||") {
		t.Fatalf("a paused archive must succeed without copying, or Postgres keeps every segment: %s", pitrArchiveCommand)
	}
}

func TestPITRSupported(t *testing.T) {
	for _, c := range []struct {
		d    dbRow
		want bool
	}{
		{dbRow{Engine: "postgres"}, true},
		{dbRow{Engine: "postgres", Stack: "shop"}, true},
		{dbRow{Engine: "mysql"}, false},
		{dbRow{Engine: "postgres", Image: "postgres:16-alpine", Volume: "data"}, true},
		{dbRow{Engine: "postgres", Image: "postgres:16-alpine"}, false},
		{dbRow{Engine: "postgres", PreviewApp: "api-feat"}, false},
	} {
		openTestDB(t)
		if got := pitrSupported(c.d); got != c.want {
			t.Errorf("%+v: got %v", c.d, got)
		}
	}
}

func TestPITRErrorShows(t *testing.T) {
	openTestDB(t)
	d := dbRow{ID: "db", Team: "t", Name: "shop", Engine: "postgres"}
	if _, err := sqlDB.Exec(`INSERT INTO pitr(db_id,enabled) VALUES('db',1)`); err != nil {
		t.Fatal(err)
	}
	p := &Panel{}
	p.notePITRError(d, errors.New("FATAL: the database system is starting up"))
	if got := getPITRStatus(d).Error; got != "" {
		t.Fatalf("a restart counted as a failure: %q", got)
	}
	p.notePITRError(d, errors.New("no pg_hba.conf entry for replication"))
	if got := getPITRStatus(d).Error; !strings.Contains(got, "pg_hba") {
		t.Fatalf("the failure doesn't show: %q", got)
	}
}
