package panel

import (
	"slices"
	"testing"
	"time"

	"github.com/nuelScript/skiff/internal/registry"
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

func TestLogTargets(t *testing.T) {
	single := logTargets("shop", registry.App{Container: "c1"}, nil, nil)
	if len(single) != 1 || single[0].prefix() != "" {
		t.Fatalf("a lone container shouldn't be labelled: %+v", single)
	}
	many := logTargets("shop", registry.App{Container: "c1", Replicas: []registry.Replica{{Container: "c1"}, {Container: "c2"}}}, []string{"skiff-shop-mailer-a", "skiff-shop-mailer-b", "left-behind"}, []string{"mailer"})
	want := []logTarget{{"c1", "shop 1"}, {"c2", "shop 2"}, {"skiff-shop-mailer-a", "mailer"}, {"skiff-shop-mailer-b", "mailer 2"}, {"left-behind", "left-behind"}}
	if !slices.Equal(many, want) {
		t.Fatalf("got %+v, want %+v", many, want)
	}
}

func TestWorkerOf(t *testing.T) {
	names := []string{"job", "job-email"}
	if got := workerOf("shop", "skiff-shop-job-email-abc", names); got != "job-email" {
		t.Fatalf("longest match should win, got %q", got)
	}
	if got := workerOf("shop", "skiff-shop-job-abc", names); got != "job" {
		t.Fatalf("got %q", got)
	}
	if got := workerOf("shop", "skiff-other-job-abc", names); got != "" {
		t.Fatalf("another app's container matched %q", got)
	}
}
