package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSafeNextRejectsControlCharsAndBackslashes(t *testing.T) {
	for _, in := range []string{"/\t/evil.example", "/\n/evil.example", "/a\r/b", "/a\\b", "/ok\x7f"} {
		if got := safeNext(in); got != "/" {
			t.Errorf("safeNext(%q) = %q, want /", in, got)
		}
	}
}

func unlockFrom(rt *Router, remote string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "https://shop-feat.example.com"+unlockPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = remote
	w := httptest.NewRecorder()
	rt.ServeHTTP(w, req)
	return w
}

func TestPreviewGuessesCountPerSubnetAndPerApp(t *testing.T) {
	rt := gatedRouter(t)
	wrong := url.Values{"password": {"guess"}}
	for i := 0; i < maxFailures; i++ {
		unlockFrom(rt, fmt.Sprintf("[2001:db8:1:2::%x]:1000", i+1), wrong)
	}
	if w := unlockFrom(rt, "[2001:db8:1:2::ffff]:1000", url.Values{"password": {"letmein123"}}); w.Code != http.StatusTooManyRequests {
		t.Fatalf("addresses in one /64 should share a budget: %d", w.Code)
	}

	rt = gatedRouter(t)
	for i := 0; i < maxAppFailures; i++ {
		rt.noteFailure(fmt.Sprintf("shop-feat|10.0.%d.1", i), "shop-feat")
	}
	if w := unlockFrom(rt, "192.0.2.77:1000", url.Values{"password": {"letmein123"}}); w.Code != http.StatusTooManyRequests {
		t.Fatalf("an app's total guesses should be capped: %d", w.Code)
	}
}

func TestGateFailureTableEvictsOldestNotEveryone(t *testing.T) {
	var g gateState
	g.failures = map[string][]time.Time{}
	now := time.Now()
	for i := 0; i < maxGateKeys; i++ {
		g.failures[fmt.Sprintf("k%d", i)] = []time.Time{now.Add(time.Duration(i) * time.Millisecond)}
	}
	g.evictOldest()
	if len(g.failures) < maxGateKeys*8/10 {
		t.Fatalf("eviction wiped too much: %d left", len(g.failures))
	}
	if _, ok := g.failures["k0"]; ok {
		t.Fatal("the oldest entry should go first")
	}
	if _, ok := g.failures[fmt.Sprintf("k%d", maxGateKeys-1)]; !ok {
		t.Fatal("the newest entry should stay")
	}
}

func TestUnlockShedsLoadWhenHashingIsBusy(t *testing.T) {
	rt := gatedRouter(t)
	for i := 0; i < maxGateWork; i++ {
		gateWork <- struct{}{}
	}
	defer func() {
		for i := 0; i < maxGateWork; i++ {
			<-gateWork
		}
	}()
	start := time.Now()
	w := unlockFrom(rt, "192.0.2.1:1000", url.Values{"password": {"letmein123"}})
	if w.Code != http.StatusServiceUnavailable || time.Since(start) > 4*time.Second {
		t.Fatalf("want a quick 503, got %d after %v", w.Code, time.Since(start))
	}
}
