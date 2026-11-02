package panel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nuelScript/skiff/internal/edge"
)

func TestProjectAttention(t *testing.T) {
	failed := &Deploy{Status: "failed", Started: 100}
	live := &Deploy{Status: "live"}
	building := &Deploy{Status: "building"}
	cases := []struct {
		name     string
		state    string
		deployed bool
		last     *Deploy
		kind     string
		serving  bool
	}{
		{"healthy", "running", true, live, "", false},
		{"asleep is fine", "sleeping", true, live, "", false},
		{"never deployed yet", "missing", false, nil, "", false},
		{"first deploy failed", "missing", false, failed, "deploy.failed", false},
		{"failed, old version up", "running", true, failed, "deploy.failed", true},
		{"failed, old version asleep", "sleeping", true, failed, "deploy.failed", true},
		{"failed and down", "exited", true, failed, "deploy.failed", false},
		{"stopped", "exited", true, live, "app.down", false},
		{"a deploy in flight waits for its result", "exited", true, building, "", false},
	}
	for _, c := range cases {
		item, ok := projectAttention("a", c.state, c.deployed, c.last)
		if ok != (c.kind != "") || item.Kind != c.kind || item.Serving != c.serving {
			t.Errorf("%s: %+v ok=%v", c.name, item, ok)
		}
	}
}

func TestUsageAttention(t *testing.T) {
	u := func(disk, mem int64) serverUsage {
		return serverUsage{Disk: resourceUsage{Total: 100, Used: disk}, Mem: resourceUsage{Total: 100, Used: mem}}
	}
	if got := usageAttention(u(84, 89)); len(got) != 0 {
		t.Fatalf("under both limits: %+v", got)
	}
	got := usageAttention(u(85, 90))
	if len(got) != 2 || got[0].Kind != "disk" || got[0].Percent != 85 || got[1].Kind != "memory" {
		t.Fatalf("at both limits: %+v", got)
	}
	// A platform that can't report memory (total 0) is no alarm.
	if got := usageAttention(serverUsage{}); len(got) != 0 {
		t.Fatalf("no data: %+v", got)
	}
}

func TestBuildConcurrency(t *testing.T) {
	const gb = int64(1 << 30)
	for _, c := range []struct {
		cpus int
		mem  int64
		want int
	}{{2, 4 * gb, 1}, {4, 8 * gb, 2}, {16, 8 * gb, 4}, {16, 64 * gb, 8}, {1, gb, 1}, {8, 0, 4}} {
		if got := buildConcurrency(c.cpus, c.mem); got != c.want {
			t.Errorf("%d cpus %d GB = %d, want %d", c.cpus, c.mem/gb, got, c.want)
		}
	}
}

func TestRoomToScale(t *testing.T) {
	if roomToScale(resourceUsage{Total: 100, Used: 90}) || !roomToScale(resourceUsage{Total: 100, Used: 89}) {
		t.Fatal("the memory limit should stop scaling at 90%")
	}
	if !roomToScale(resourceUsage{}) {
		t.Fatal("a platform that reports no memory shouldn't block scaling")
	}
	if maxReplicas() < 10 {
		t.Fatalf("maxReplicas = %d", maxReplicas())
	}
}

func TestPruneRequestsByCount(t *testing.T) {
	openTestDB(t)
	now := time.Now()
	tx, _ := sqlDB.Begin()
	for i := range 50 {
		_, _ = tx.Exec(`INSERT INTO requests(t,app,method,path,status,dur,bytes,ref,visitor,bot) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			now.Add(-time.Duration(i)*time.Second).UnixMilli(), "a", "GET", "/", 200, 1, 1, "", "v", 0)
	}
	_, _ = tx.Exec(`INSERT INTO requests(t,app,method,path,status,dur,bytes,ref,visitor,bot) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		now.Add(-8*24*time.Hour).UnixMilli(), "a", "GET", "/old", 200, 1, 1, "", "v", 0)
	_ = tx.Commit()
	pruneRequests(now)
	var n int
	_ = sqlDB.QueryRow(`SELECT count(*) FROM requests`).Scan(&n)
	if n != 50 {
		t.Fatalf("rows after pruning = %d, want the 50 inside the window", n)
	}
}

func TestOOMLog(t *testing.T) {
	now := time.Now()
	if !noteOOM("oom-a", now) {
		t.Fatal("the first kill should alert")
	}
	if noteOOM("oom-a", now.Add(time.Minute)) {
		t.Fatal("a crash loop shouldn't alert every time")
	}
	if !noteOOM("oom-a", now.Add(time.Minute+oomAlertGap)) {
		t.Fatal("it should alert again after the gap")
	}
	if _, ok := recentOOM("oom-a", now.Add(2*time.Minute+oomAlertGap)); !ok {
		t.Fatal("a recent kill should show")
	}
	if _, ok := recentOOM("oom-a", now.Add(2*time.Hour)); ok {
		t.Fatal("an old kill should stop showing")
	}
	if _, ok := recentOOM("oom-never", now); ok {
		t.Fatal("an app that never ran out shouldn't show")
	}
}

func TestResourcesInToml(t *testing.T) {
	openTestDB(t)
	got := projectToml(Source{App: "a", Port: "80", Memory: "512m", CPU: "0.5"}, nil, "net", nil)
	if !strings.Contains(got, "[resources]\nmemory = \"512m\"\ncpu = \"0.5\"\n") {
		t.Fatalf("toml = %s", got)
	}
	if strings.Contains(projectToml(Source{App: "a", Port: "80"}, nil, "net", nil), "[resources]") {
		t.Fatal("no limits should mean no section")
	}
	_ = putSource(Source{App: "shop", Port: "80", Memory: "1g", CPU: "2"})
	got = projectToml(Source{App: "shop-x", Port: "80", Parent: "shop"}, nil, "net", nil)
	if !strings.Contains(got, `memory = "1g"`) {
		t.Fatalf("a preview should inherit its project's limits: %s", got)
	}
	if s, _ := getSource("shop"); s.Memory != "1g" || s.CPU != "2" {
		t.Fatalf("limits didn't round-trip: %+v", s)
	}
}

func TestWriteEdgeFile(t *testing.T) {
	openTestDB(t)
	t.Setenv("HOME", t.TempDir())
	_ = os.MkdirAll(filepath.Dir(edgeFile()), 0o755)
	_ = putSource(Source{App: "api", Port: "80", Private: true, RateLimit: 100})
	_ = putSource(Source{App: "api-feat", Port: "80", Parent: "api"})
	_ = putSource(Source{App: "web", Port: "80", RateLimit: 50})
	_ = putSource(Source{App: "plain", Port: "80"})
	if err := writeEdgeFile(); err != nil {
		t.Fatal(err)
	}
	got := edge.Load(edgeFile())
	if !got["api"].Private || got["api"].RateLimit != 100 {
		t.Errorf("api = %+v", got["api"])
	}
	if !got["api-feat"].Private {
		t.Error("a private project's preview must not be public")
	}
	if got["web"].Private || got["web"].RateLimit != 50 {
		t.Errorf("web = %+v", got["web"])
	}
	if _, listed := got["plain"]; listed {
		t.Error("an app with no rules needn't be listed")
	}
	if s, _ := getSource("api"); !s.Private || s.RateLimit != 100 {
		t.Errorf("didn't round-trip: %+v", s)
	}
	if u := internalURL("api", Source{Port: "3000"}, 0); u != "http://api:3000" {
		t.Errorf("internal url = %q", u)
	}
	if u := internalURL("api", Source{}, 8080); u != "http://api:8080" {
		t.Errorf("internal url = %q", u)
	}
}
