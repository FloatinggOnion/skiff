package panel

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProbe(t *testing.T) {
	codes := map[string]int{"/ok": 200, "/missing": 404, "/broken": 502}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/moved" {
			http.Redirect(w, r, "https://elsewhere.invalid/", http.StatusFound)
			return
		}
		w.WriteHeader(codes[r.URL.Path])
	}))
	defer srv.Close()
	for path, up := range map[string]bool{"/ok": true, "/missing": true, "/moved": true, "/broken": false} {
		if got := probe(srv.URL + path); got.OK != up {
			t.Errorf("%s: up=%v (%+v)", path, got.OK, got)
		}
	}
	if got := probe("http://127.0.0.1:1/"); got.OK || got.Err == "" {
		t.Errorf("unreachable: %+v", got)
	}
}

func TestIncidentLifecycle(t *testing.T) {
	openTestDB(t)
	u := &uptimeTracker{fails: map[string]int{}, open: map[string]int64{}}
	now := time.Now()
	step := func(ok bool) (bool, bool) {
		now = now.Add(time.Minute)
		o, c, _ := u.observe("shop", "t1", probeResult{OK: ok, Code: 502}, now)
		return o, c
	}
	if o, _ := step(false); o {
		t.Fatal("opened on the first failure")
	}
	if o, _ := step(false); !o {
		t.Fatal("didn't open on the second failure")
	}
	if o, _ := step(false); o {
		t.Fatal("opened twice")
	}
	if _, c := step(true); !c {
		t.Fatal("didn't close on recovery")
	}
	v := buildUptime("shop", now)
	if len(v.Incidents) != 1 || v.Incidents[0].Ended == 0 || v.Incidents[0].Cause != "HTTP 502" {
		t.Fatalf("incidents %+v", v.Incidents)
	}
	if !v.Up || v.Pct24h != 25 || len(v.Days) != 30 {
		t.Fatalf("up %v pct %v days %d", v.Up, v.Pct24h, len(v.Days))
	}

	u2 := &uptimeTracker{fails: map[string]int{}, open: map[string]int64{}}
	step(false)
	step(false)
	u2.loadOpen()
	if len(u.open) != 1 || len(u2.open) != 1 {
		t.Fatalf("open incidents not restored after a restart: %v %v", u.open, u2.open)
	}
}
