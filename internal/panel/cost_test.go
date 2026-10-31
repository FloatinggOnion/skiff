package panel

import (
	"math"
	"testing"
	"time"
)

func TestShare(t *testing.T) {
	const gb = int64(1 << 30)
	// a quarter of the memory and an idle CPU is an eighth of the box
	if got := share(0, gb, 2, 4*gb); math.Abs(got-0.125) > 1e-9 {
		t.Fatalf("share = %v", got)
	}
	// one full core of two plus no memory is a quarter
	if got := share(100, 0, 2, 4*gb); math.Abs(got-0.25) > 1e-9 {
		t.Fatalf("share = %v", got)
	}
	if share(50, gb, 0, 0) != 0 {
		t.Fatal("no capacity should mean no share")
	}
}

func TestDiskForecast(t *testing.T) {
	openTestDB(t)
	now := time.Now()
	const gb = int64(1 << 30)
	for h := 48; h >= 0; h-- {
		used := 20*gb + int64(48-h)*gb/24 // grows 1 GB a day
		_, _ = sqlDB.Exec(`INSERT INTO disk_samples(t,used,total) VALUES(?,?,?)`, now.Add(-time.Duration(h)*time.Hour).Unix(), used, 40*gb)
	}
	perDay, days := diskForecast(10 * gb)
	if math.Abs(float64(perDay-gb)) > float64(gb)/50 || days < 9 || days > 10 {
		t.Fatalf("perDay %d days %d", perDay, days)
	}
	openTestDB(t)
	if _, days := diskForecast(10 * gb); days != -1 {
		t.Fatal("forecast without history")
	}
}

func TestBuildCostAddsUpToThePrice(t *testing.T) {
	openTestDB(t)
	resStats = newResStore("")
	now := time.Now().Unix() / 60 * 60
	resStats.apps = map[string]map[int64]*resBucket{
		"web":    {now: {T: now, CPUSum: 20, MemSum: 400 << 20, N: 1}},
		"db:x1":  {now: {T: now, CPUSum: 2, MemSum: 100 << 20, N: 1}},
		"orphan": {now: {T: now, CPUSum: 90, MemSum: 900 << 20, N: 1}},
	}
	putSource(Source{App: "web", Team: "t1", Port: "3000"})
	_ = putDB(dbRow{ID: "x1", Team: "t1", Name: "main", Engine: "postgres", Container: "c", Host: "c", Port: 5432})
	_ = setSetting("server_cost", "10.00")

	rep := (&Panel{}).buildCost()
	if len(rep.Items) != 2 {
		t.Fatalf("items %+v", rep.Items)
	}
	if rep.MemTotal == 0 {
		t.Skip("no /proc/meminfo here; totals are only meaningful on Linux")
	}
	if rep.Items[0].Name != "web" || rep.Items[1].Kind != "database" {
		t.Fatalf("items not ordered by share: %+v", rep.Items)
	}
	sum := rep.System + rep.Idle
	for _, it := range rep.Items {
		sum += it.Monthly
	}
	if math.Abs(sum-10) > 0.05 {
		t.Fatalf("parts add up to %.2f of 10.00", sum)
	}
}
