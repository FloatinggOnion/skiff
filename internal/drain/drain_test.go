package drain

import (
	"testing"
	"time"
)

func TestActiveDropsExpiredAndRemoved(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Now()
	for _, e := range []Entry{
		{App: "a", Container: "live", Until: now.Add(time.Minute).Unix()},
		{App: "a", Container: "gone", Until: now.Add(time.Minute).Unix()},
		{App: "a", Container: "expired", Until: now.Add(-time.Minute).Unix()},
	} {
		if err := Add(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := Remove("gone"); err != nil {
		t.Fatal(err)
	}
	got := Active(now)
	if !got["live"] || got["gone"] || got["expired"] {
		t.Fatalf("active: %v", got)
	}
	if len(Load()) != 2 {
		t.Fatalf("expired stays listed until retired: %+v", Load())
	}
}

func TestRouterAliveNeedsARecentBeat(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if RouterAlive() {
		t.Fatal("alive before any beat")
	}
	if err := Add(Entry{}); err != nil { // creates the directory
		t.Fatal(err)
	}
	Beat()
	if !RouterAlive() {
		t.Fatal("not alive after a beat")
	}
}
