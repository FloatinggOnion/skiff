package panel

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIngestAndInsights(t *testing.T) {
	openTestDB(t)
	dir := t.TempDir()
	now := time.Now()
	day := now.UTC().Format("2006-01-02")
	file := filepath.Join(dir, day+".jsonl")
	ms := now.Add(-10 * time.Minute).UnixMilli()
	lines := ""
	add := func(path string, status int, dur int64, visitor, ref string, bot bool) {
		lines += fmt.Sprintf(`{"t":%d,"a":"shop","m":"GET","p":%q,"s":%d,"d":%d,"b":100,"r":%q,"v":%q,"bt":%v}`+"\n",
			ms, path, status, dur, ref, visitor, bot)
	}
	add("/", 200, 10, "v1", "news.ycombinator.com", false)
	add("/", 200, 20, "v2", "news.ycombinator.com", false)
	add("/pricing", 200, 30, "v1", "", false)
	add("/_next/static/app.js", 200, 5, "v1", "", false)
	add("/api/cart", 500, 400, "v2", "", false)
	add("/.env", 404, 1, "scan", "", true)
	lines += `{"t":1,"a":"shop"` // partial line the router is still writing
	if err := os.WriteFile(file, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}

	if n, err := ingestRequests(dir); err != nil || n != 6 {
		t.Fatalf("ingested %d, %v", n, err)
	}
	if n, _ := ingestRequests(dir); n != 0 {
		t.Fatalf("re-ingested %d lines", n)
	}
	f, _ := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0o600)
	fmt.Fprintf(f, `,"m":"GET","p":"/late","s":200,"d":1,"v":"v3"}`+"\n")
	f.Close()
	if n, _ := ingestRequests(dir); n != 1 {
		t.Fatalf("finishing the partial line should ingest it once, got %d", n)
	}

	addDeploy(Deploy{ID: "d1", App: "shop", Trigger: "cli", Status: "live", Started: now.Add(-5 * time.Minute).Unix()})
	in, err := buildInsights("shop", "1h", now)
	if err != nil {
		t.Fatal(err)
	}
	if in.Requests != 6 || in.Errors5 != 1 || in.Errors4 != 1 || in.Bots != 1 {
		t.Fatalf("totals %+v", in)
	}
	if in.Visitors != 2 || in.Pageviews != 3 {
		t.Fatalf("visitors %d pageviews %d", in.Visitors, in.Pageviews)
	}
	if in.P99 != 400 || in.P50 != 10 {
		t.Fatalf("p50 %d p99 %d", in.P50, in.P99)
	}
	if len(in.Referrers) != 1 || in.Referrers[0].Key != "news.ycombinator.com" || in.Referrers[0].Count != 2 {
		t.Fatalf("referrers %+v", in.Referrers)
	}
	if len(in.ErrorPaths) != 1 || in.ErrorPaths[0].Key != "/api/cart 500" {
		t.Fatalf("error paths %+v", in.ErrorPaths)
	}
	if len(in.Deploys) != 1 || len(in.Series) < 60 {
		t.Fatalf("deploys %d series %d", len(in.Deploys), len(in.Series))
	}

	pruneRequests(now.Add(8 * 24 * time.Hour))
	if again, _ := buildInsights("shop", "7d", now); again.Requests != 0 {
		t.Fatalf("%d requests survived pruning", again.Requests)
	}
}

func TestSpoofedReferrersArentCountedAsPeople(t *testing.T) {
	openTestDB(t)
	now := time.Now()
	add := func(path, ref, visitor string) {
		t.Helper()
		if _, err := sqlDB.Exec(`INSERT INTO requests(t,app,method,path,status,ref,visitor,bot) VALUES(?,?,?,?,200,?,?,0)`,
			now.UnixMilli(), "web", "GET", path, ref, visitor); err != nil {
			t.Fatal(err)
		}
	}
	add("/", "baidu.xyz", "scanner")
	add("/_next/static/chunks/a.js", "baidu.xyz", "scanner")
	add("/", "news.ycombinator.com", "reader")
	add("/_next/static/chunks/a.js", "", "reader")
	add("/logo.png", "someblog.example", "hotlinker")

	got, err := buildInsights("web", "24h", now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Pageviews != 1 {
		t.Errorf("pageviews = %d, want 1", got.Pageviews)
	}
	if got.Bots != 2 {
		t.Errorf("bots = %d, want 2", got.Bots)
	}
	refs := map[string]bool{}
	for _, r := range got.Referrers {
		refs[r.Key] = true
	}
	if refs["baidu.xyz"] || !refs["news.ycombinator.com"] || !refs["someblog.example"] {
		t.Errorf("referrers = %+v", got.Referrers)
	}
}
