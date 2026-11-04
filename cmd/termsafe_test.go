package cmd

import (
	"bytes"
	"testing"
)

func TestStripControl(t *testing.T) {
	cases := map[string]string{
		"plain\tline\n":                   "plain\tline\n",
		"\x1b[31mred\x1b[0m":              "red",
		"a\x1b]0;title\x07b":              "ab",
		"a\x1b]8;;http://x\x1b\\link":     "alink",
		"bell\x07 and\rcr":                "bell andcr",
		"c1\u009b31mx":                    "c131mx",
		"héllo ✓":                         "héllo ✓",
		"\x1bPdevice\x1b\\ok":             "ok",
		"\x1b[2J\x1b[Hhidden\x08\x08\x08": "hidden",
	}
	for in, want := range cases {
		if got := stripControl(in); got != want {
			t.Errorf("stripControl(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStripWriterSplitAcrossWrites(t *testing.T) {
	var buf bytes.Buffer
	w := &stripWriter{w: &buf}
	for _, part := range []string{"a\x1b", "[3", "1mred\x1b]0;t", "itle\x07z"} {
		if _, err := w.Write([]byte(part)); err != nil {
			t.Fatal(err)
		}
	}
	if got := buf.String(); got != "aredz" {
		t.Fatalf("got %q", got)
	}
}
