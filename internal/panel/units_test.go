package panel

import (
	"strings"
	"testing"
)

func TestDesiredReplicas(t *testing.T) {
	cases := []struct {
		cpu, target      float64
		min, max, expect int
	}{
		{0, 70, 1, 5, 1},   // idle → floor
		{180, 50, 1, 5, 4}, // ceil(180/50)=4
		{500, 50, 1, 3, 3}, // clamp to max
		{40, 70, 2, 5, 2},  // below target but min floor
		{71, 70, 1, 5, 2},  // just over one replica's worth
		{140, 70, 1, 5, 2}, // exactly two
	}
	for _, c := range cases {
		if got := desiredReplicas(c.cpu, c.target, c.min, c.max); got != c.expect {
			t.Errorf("desiredReplicas(%v,%v,%d,%d) = %d, want %d", c.cpu, c.target, c.min, c.max, got, c.expect)
		}
	}
}

func TestScaleBounds(t *testing.T) {
	min, max, target := scaleBounds(Source{})
	if min != 1 || max != 1 || target != 70 {
		t.Errorf("empty source bounds = %d,%d,%v; want 1,1,70", min, max, target)
	}
	min, max, _ = scaleBounds(Source{ScaleMin: 3, ScaleMax: 2}) // max < min
	if min != 3 || max != 3 {
		t.Errorf("max<min not normalized: %d,%d", min, max)
	}
	_, max, _ = scaleBounds(Source{ScaleMin: 1, ScaleMax: 99}) // over cap
	if max != maxReplicas() {
		t.Errorf("max cap = %d, want %d", max, maxReplicas())
	}
}

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"My-App":      "my-app",
		"UPPER_case!": "uppercase",
		"  spaced  ":  "spaced",
		"a/b\\c":      "abc",
		"keep-123":    "keep-123",
	}
	for in, want := range cases {
		if got := sanitizeName(in); got != want {
			t.Errorf("sanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTeamNetwork(t *testing.T) {
	if got := teamNetwork(""); got != dbNetwork {
		t.Errorf("empty team → %q, want shared %q", got, dbNetwork)
	}
	if got := teamNetwork("abc123"); got != "skiff-t-abc123" {
		t.Errorf("teamNetwork = %q", got)
	}
}

func TestDatabaseURLs(t *testing.T) {
	pg := dbEngines["postgres"]
	if got := pg.url("h", 5432, "u", "p", "d", false); !strings.Contains(got, "sslmode=disable") {
		t.Errorf("postgres private URL should be plaintext: %s", got)
	}
	if got := pg.url("h", 5432, "u", "p", "d", true); !strings.Contains(got, "sslmode=require") {
		t.Errorf("postgres public URL should require TLS: %s", got)
	}
	if got := dbEngines["mysql"].url("h", 3306, "u", "p", "d", true); !strings.Contains(got, "ssl-mode=REQUIRED") {
		t.Errorf("mysql public URL should require TLS: %s", got)
	}
	if got := dbEngines["mongodb"].url("h", 27017, "u", "p", "d", true); !strings.Contains(got, "tls=true") {
		t.Errorf("mongodb public URL should enable TLS: %s", got)
	}
}

func TestHasBucket(t *testing.T) {
	list := "  media-files\tsize:0\tlogical:0\tchunk:0\n  media\tsize:0\n"
	if !hasBucket(list, "media-files") || !hasBucket(list, "media") || hasBucket(list, "medi") || hasBucket("", "media") {
		t.Fatal("hasBucket matched the wrong names")
	}
}
