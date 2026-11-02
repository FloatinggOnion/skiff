package panel

import "testing"

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
