package panel

import (
	"errors"
	"io"
	"net"
	"strings"
	"testing"
)

func TestDialFilterCoversSharedAndTranslatedRanges(t *testing.T) {
	for _, s := range []string{"100.64.0.1", "100.127.255.254", "192.0.0.8", "64:ff9b::7f00:1", "2002:7f00:1::1"} {
		if !isBlockedDialIP(net.ParseIP(s)) {
			t.Errorf("%s should be blocked", s)
		}
	}
	for _, s := range []string{"100.128.0.1", "192.0.2.1", "2606:4700::1"} {
		if isBlockedDialIP(net.ParseIP(s)) {
			t.Errorf("%s should be allowed", s)
		}
	}
}

func fakeExec(files map[string]string) func([]string, io.Writer) error {
	return func(cmd []string, out io.Writer) error {
		body, ok := files[cmd[len(cmd)-1]]
		if !ok {
			return errors.New("no such file")
		}
		if cmd[0] == "stat" {
			_, _ = io.WriteString(out, body[:strings.IndexByte(body, '|')]+"\n")
			return nil
		}
		_, err := io.WriteString(out, body[strings.IndexByte(body, '|')+1:])
		return err
	}
}

func TestReadSegmentRefusesOversizedFiles(t *testing.T) {
	run := fakeExec(map[string]string{
		pitrMount + "/ok.gz":   "5|hello",
		pitrMount + "/huge.gz": "999999999999|x",
		pitrMount + "/liar.gz": "5|" + strings.Repeat("x", maxSegmentBytes+1),
		pitrMount + "/junk.gz": "abc|x",
	})
	if b, err := readSegment(run, "ok.gz"); err != nil || string(b) != "hello" {
		t.Fatalf("normal file: %q %v", b, err)
	}
	for _, n := range []string{"huge.gz", "liar.gz", "junk.gz", "missing.gz"} {
		if _, err := readSegment(run, n); err == nil {
			t.Errorf("%s should be refused", n)
		}
	}
}
