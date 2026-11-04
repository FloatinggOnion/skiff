package router

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nuelScript/skiff/internal/docker"
)

func wakeRouter(t *testing.T) *Router {
	t.Helper()
	t.Setenv("PATH", t.TempDir()) // no docker binary, so engine calls fail at once
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	t.Cleanup(backend.Close)
	port, _ := strconv.Atoi(backend.URL[strings.LastIndexByte(backend.URL, ':')+1:])
	sleepFile := filepath.Join(t.TempDir(), "sleeping.json")
	_ = os.WriteFile(sleepFile, []byte(`{"dead":["c1"],"alive":["c2"]}`), 0o644)
	rt := &Router{Domains: []string{"example.com"}, SleepFile: sleepFile, Engine: docker.Remote("nowhere")}
	rt.routeTable = map[string][]int{"alive": {port}}
	rt.routeAt = time.Now().Add(time.Hour)
	old := wakeTimeout
	wakeTimeout = time.Second
	t.Cleanup(func() { wakeTimeout = old })
	return rt
}

func TestWakeOfOneAppDoesNotBlockOthers(t *testing.T) {
	rt := wakeRouter(t)
	done := make(chan struct{})
	go func() { rt.wake("dead"); close(done) }()
	time.Sleep(100 * time.Millisecond)

	start := time.Now()
	if len(rt.wake("alive")) == 0 || time.Since(start) > 500*time.Millisecond {
		t.Fatalf("a healthy app waited on another's wake (%v)", time.Since(start))
	}
	<-done

	start = time.Now()
	if rt.wake("dead") != nil || time.Since(start) > 200*time.Millisecond {
		t.Fatalf("a wake that just failed should be remembered (%v)", time.Since(start))
	}
}

func TestWakeQueueIsCapped(t *testing.T) {
	rt := wakeRouter(t)
	st := rt.wakeStateFor("dead")
	for i := 0; i < maxWakeWaiters; i++ {
		if !rt.enterWake(st) {
			t.Fatalf("waiter %d refused", i)
		}
	}
	if rt.enterWake(st) {
		t.Fatal("waiters beyond the cap must be refused")
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); rt.wake("dead") }()
	wg.Wait()
}
