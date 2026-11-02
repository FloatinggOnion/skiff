package docker

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
	"time"
)

func frame(kind byte, payload string) []byte {
	h := make([]byte, 8)
	h[0] = kind
	binary.BigEndian.PutUint32(h[4:], uint32(len(payload)))
	return append(h, payload...)
}

func TestDemuxLogs(t *testing.T) {
	var in bytes.Buffer
	in.Write(frame(1, "2026-11-02T10:00:00.5Z hello\n"))
	in.Write(frame(2, "2026-11-02T10:00:01Z oh no\n2026-11-02T10:00:02Z "))
	in.Write(frame(1, "2026-11-02T10:00:03Z split "))
	in.Write(frame(2, "second\n"))
	in.Write(frame(1, "line\r\n"))
	in.Write(frame(1, "not a timestamp\n"))
	type got struct{ stream, text string }
	var lines []got
	var stamps []time.Time
	if err := demuxLogs(&in, func(ts time.Time, stream, text string) {
		lines = append(lines, got{stream, text})
		stamps = append(stamps, ts)
	}); err != nil {
		t.Fatal(err)
	}
	want := []got{{"out", "hello"}, {"err", "oh no"}, {"err", "second"}, {"out", "split line"}, {"out", "not a timestamp"}}
	if len(lines) != len(want) {
		t.Fatalf("lines = %+v", lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %+v, want %+v", i, lines[i], want[i])
		}
	}
	if stamps[0].Nanosecond() != 500_000_000 || stamps[0].Year() != 2026 {
		t.Errorf("timestamp = %v", stamps[0])
	}
}

func TestDemuxLogsTruncatedFrame(t *testing.T) {
	b := frame(1, strings.Repeat("x", 100))
	if err := demuxLogs(bytes.NewReader(b[:50]), func(time.Time, string, string) { t.Error("no complete line") }); err != nil {
		t.Fatal(err)
	}
}

func TestRoutesFromList(t *testing.T) {
	c := apiContainer{Labels: map[string]string{"skiff.app": "shop", "skiff.port": "80"}}
	c.Ports = append(c.Ports, struct {
		PrivatePort int
		PublicPort  int
		Type        string
	}{443, 9001, "tcp"}, struct {
		PrivatePort int
		PublicPort  int
		Type        string
	}{80, 9000, "tcp"})
	noPort := apiContainer{Labels: map[string]string{"skiff.app": "stopping", "skiff.port": "80"}}
	badLabel := apiContainer{Labels: map[string]string{"skiff.app": "x", "skiff.port": "nope"}}
	got := routesFromList([]apiContainer{c, noPort, badLabel})
	if len(got) != 1 || got[0].App != "shop" || got[0].HostPort != 9000 {
		t.Fatalf("routes = %+v", got)
	}
}
