package docker

import "testing"

func TestParsePercent(t *testing.T) {
	cases := map[string]float64{
		"0.00%": 0, "12.34%": 12.34, "100.09%": 100.09, "  5.5% ": 5.5, "--": 0, "": 0,
	}
	for in, want := range cases {
		if got := parsePercent(in); got != want {
			t.Errorf("parsePercent(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParseSize(t *testing.T) {
	unit := func(v float64, shift uint) int64 { return int64(v * float64(int64(1)<<shift)) }
	cases := []struct {
		in   string
		want int64
	}{
		{"0B", 0},
		{"512B", 512},
		{"15.87MiB", unit(15.87, 20)},
		{"7.63GiB", unit(7.63, 30)},
		{"1.5KiB", unit(1.5, 10)},
		{"2GB", 2 << 30},
		{"", 0},
	}
	for _, c := range cases {
		if got := parseSize(c.in); got != c.want {
			t.Errorf("parseSize(%q) = %d, want %d", c.in, got, c.want)
		}
	}
	// Trailing space (from the "used / limit" split) must be trimmed.
	if got := parseSize("15.87MiB "); got != unit(15.87, 20) {
		t.Errorf("trailing space not trimmed: %d", got)
	}
}

func TestParseSkiffContainers(t *testing.T) {
	out := []byte("portfolio-v1-f9d05b|2026-07-05 10:56:01 +0000 UTC|portfolio-v1|3000|running\n" +
		"pg-main|2026-07-04 22:30:00 +0000 UTC|||exited\n" +
		"\n")
	cs := parseSkiffContainers(out)
	if len(cs) != 2 {
		t.Fatalf("got %d containers, want 2: %+v", len(cs), cs)
	}
	app := cs[0]
	if app.Name != "portfolio-v1-f9d05b" || app.App != "portfolio-v1" || app.Port != 3000 || !app.Running {
		t.Errorf("app container parsed wrong: %+v", app)
	}
	if app.Created.IsZero() {
		t.Errorf("created time not parsed")
	}
	if db := cs[1]; db.App != "" || db.Port != 0 || db.Running {
		t.Errorf("unlabelled stopped container parsed wrong: %+v", db)
	}
}

func TestParseCreatedAt(t *testing.T) {
	got := parseCreatedAt("2026-09-23 19:54:00 +0000 UTC")
	if got.Year() != 2026 || got.Month() != 9 || got.Hour() != 19 || got.Minute() != 54 {
		t.Fatalf("parsed %v", got)
	}
}

func TestParseVolumeSizes(t *testing.T) {
	out := []byte(`[{"Name":"skiff-app_data","Size":"64.63MB"},{"Name":"skiff-npm","Size":"0B"}]` + "\n")
	got := parseVolumeSizes(out)
	want := 64.63 * (1 << 20)
	if got["skiff-app_data"] != int64(want) || got["skiff-npm"] != 0 || len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	if len(parseVolumeSizes([]byte("null"))) != 0 {
		t.Fatal("null output should give no sizes")
	}
}
