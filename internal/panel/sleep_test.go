package panel

import (
	"testing"
	"time"
)

func TestShouldSleep(t *testing.T) {
	cases := []struct {
		src  Source
		idle time.Duration
		want bool
	}{
		{Source{SleepAfter: 30}, 31 * time.Minute, true},
		{Source{SleepAfter: 30}, 29 * time.Minute, false},
		{Source{SleepAfter: 0}, 48 * time.Hour, false},
		{Source{SleepAfter: 30, Autoscale: true}, 48 * time.Hour, false},
	}
	for _, c := range cases {
		if got := shouldSleep(c.src, c.idle); got != c.want {
			t.Errorf("shouldSleep(%+v, %v) = %v", c.src, c.idle, got)
		}
	}
}

func TestLastActivityIgnoresBots(t *testing.T) {
	openTestDB(t)
	human := time.Now().Add(-2 * time.Hour).Truncate(time.Millisecond)
	bot := time.Now().Add(-time.Minute)
	_, _ = sqlDB.Exec(`INSERT INTO requests(t,app,bot) VALUES(?,?,0),(?,?,1)`, human.UnixMilli(), "web", bot.UnixMilli(), "web")
	prev := panelStarted
	panelStarted = time.Now().Add(-24 * time.Hour)
	defer func() { panelStarted = prev }()
	if got := lastActivity("web"); !got.Equal(human) {
		t.Fatalf("last activity %v, want the human visit %v", got, human)
	}
	addDeploy(Deploy{ID: "d", App: "web", Status: "live", Started: time.Now().Unix()})
	if got := lastActivity("web"); time.Since(got) > time.Minute {
		t.Fatalf("a fresh deploy should count as activity, got %v", got)
	}
}
