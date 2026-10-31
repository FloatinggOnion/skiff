package panel

import (
	"testing"
	"time"
)

func TestAppLogWriteAndSearch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	w := &logWriter{}
	day1 := time.Date(2026, 10, 30, 23, 59, 0, 0, time.UTC)
	day2 := day1.Add(2 * time.Minute)
	w.write("shop", "shop-a1", day1, "out", "GET / 200")
	w.write("shop", "shop-a1", day1.Add(time.Second), "err", "Error: db timeout\tretrying")
	w.write("shop", "shop-b2", day2, "out", "GET /cart 500")
	w.write("shop", "shop-b2", day2.Add(time.Second), "err", "panic: nil map")

	if got := lastLoggedAt("shop"); !got.Equal(day2.Add(time.Second)) {
		t.Fatalf("lastLoggedAt = %v", got)
	}
	all, more := searchAppLogs("shop", "", false, day1.Add(-time.Hour), day2.Add(time.Hour), 100)
	if len(all) != 4 || more || all[0].Text != "GET / 200" || all[3].Text != "panic: nil map" {
		t.Fatalf("all = %+v", all)
	}
	if all[1].Text != "Error: db timeout    retrying" {
		t.Fatalf("tabs in a line must not break the format: %q", all[1].Text)
	}
	errs, _ := searchAppLogs("shop", "", true, day1.Add(-time.Hour), day2.Add(time.Hour), 100)
	if len(errs) != 2 {
		t.Fatalf("errors = %+v", errs)
	}
	q, _ := searchAppLogs("shop", "CART", false, day1.Add(-time.Hour), day2.Add(time.Hour), 100)
	if len(q) != 1 || q[0].Container != "shop-b2" {
		t.Fatalf("query = %+v", q)
	}
	newest, more := searchAppLogs("shop", "", false, day1.Add(-time.Hour), day2.Add(time.Hour), 2)
	if !more || len(newest) != 2 || newest[1].Text != "panic: nil map" {
		t.Fatalf("limit should keep the newest lines: %+v %v", newest, more)
	}
	window, _ := searchAppLogs("shop", "", false, day2, day2.Add(time.Hour), 100)
	if len(window) != 2 {
		t.Fatalf("time window = %+v", window)
	}
}

func TestAppLogDayCap(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	w := &logWriter{}
	now := time.Now()
	w.write("big", "c", now, "out", "first")
	w.size = appLogDayCap
	w.write("big", "c", now, "out", "dropped")
	w.write("big", "c", now, "out", "dropped too")
	lines, _ := searchAppLogs("big", "", false, now.Add(-time.Hour), now.Add(time.Hour), 100)
	if len(lines) != 2 || lines[1].Container != "skiff" {
		t.Fatalf("lines = %+v", lines)
	}
}
