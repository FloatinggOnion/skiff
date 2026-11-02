package router

import (
	"bufio"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReqLogWritesDailyFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "requests")
	l := NewReqLog(dir)
	go l.Run()

	r := httptest.NewRequest("GET", "https://shop.example.com/cart?id=1", nil)
	r.RemoteAddr = "203.0.113.9:4242"
	r.Header.Set("User-Agent", "Mozilla/5.0")
	r.Header.Set("Referer", "https://news.ycombinator.com/item?id=1")
	l.Record("shop", r, 200, 512, 30*time.Millisecond)
	bot := httptest.NewRequest("GET", "https://shop.example.com/.env", nil)
	bot.Header.Set("User-Agent", "zgrab/0.x")
	l.Record("shop", bot, 404, 0, time.Millisecond)

	file := filepath.Join(dir, time.Now().UTC().Format("2006-01-02")+".jsonl")
	var lines []ReqEntry
	for i := 0; i < 50 && len(lines) < 2; i++ {
		time.Sleep(100 * time.Millisecond)
		lines = nil
		if f, err := os.Open(file); err == nil {
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				var e ReqEntry
				if json.Unmarshal(sc.Bytes(), &e) == nil {
					lines = append(lines, e)
				}
			}
			f.Close()
		}
	}
	if len(lines) != 2 {
		t.Fatalf("got %d entries", len(lines))
	}
	e := lines[0]
	if e.A != "shop" || e.P != "/cart" || e.S != 200 || e.R != "news.ycombinator.com" || e.Bt || len(e.V) != 12 {
		t.Fatalf("entry = %+v", e)
	}
	if !lines[1].Bt {
		t.Fatal("scanner not flagged as a bot")
	}
	if l.visitor("1.2.3.4", "ua", time.Now()) == l.visitor("1.2.3.5", "ua", time.Now()) {
		t.Fatal("visitor hash ignores the ip")
	}
}

func TestReqLogPrunesOldDays(t *testing.T) {
	dir := t.TempDir()
	l := &ReqLog{dir: dir}
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	for _, d := range []string{"2026-10-20", "2026-10-28", "2026-10-29", "2026-10-31"} {
		_ = os.WriteFile(filepath.Join(dir, d+".jsonl"), nil, 0o600)
	}
	l.prune(now)
	left, _ := os.ReadDir(dir)
	if len(left) != 2 {
		t.Fatalf("kept %d files", len(left))
	}
}
