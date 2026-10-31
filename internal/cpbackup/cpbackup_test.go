package cpbackup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	src := t.TempDir()
	_ = os.WriteFile(filepath.Join(src, "github.json"), []byte(`{"id":1}`), 0o600)
	_ = os.MkdirAll(filepath.Join(src, "deploys", "web"), 0o700)
	_ = os.WriteFile(filepath.Join(src, "deploys", "web", "d1.log"), []byte("built"), 0o600)
	_ = os.WriteFile(filepath.Join(src, "metrics.json"), []byte("not in the bundle"), 0o600)
	snap := filepath.Join(t.TempDir(), "snap.db")
	_ = os.WriteFile(snap, []byte("sqlite bytes"), 0o600)

	var buf bytes.Buffer
	if err := Pack(&buf, src, snap); err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	sealed, err := Seal(key, buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte(`"id":1`)) {
		t.Fatal("bundle isn't encrypted")
	}

	wrong := make([]byte, 32)
	if _, err := Open(wrong, sealed); err == nil {
		t.Fatal("opened with the wrong key")
	}
	plain, err := Open(key, sealed)
	if err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	names, err := Unpack(bytes.NewReader(plain), dst)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 3 {
		t.Fatalf("unpacked %v", names)
	}
	for file, want := range map[string]string{DBName: "sqlite bytes", "github.json": `{"id":1}`, "deploys/web/d1.log": "built"} {
		if got, _ := os.ReadFile(filepath.Join(dst, file)); string(got) != want {
			t.Errorf("%s = %q", file, got)
		}
	}
	if _, err := os.Stat(filepath.Join(dst, "metrics.json")); err == nil {
		t.Error("metrics.json shouldn't be in a bundle")
	}
}

func TestUnpackRefusesEscapingPaths(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "../../evil", Mode: 0o600, Size: 1})
	_, _ = tw.Write([]byte("x"))
	_ = tw.Close()
	_ = gz.Close()
	if _, err := Unpack(&buf, t.TempDir()); err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseKey(t *testing.T) {
	if _, err := ParseKey(strings.Repeat("ab", 32)); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseKey("short"); err == nil {
		t.Fatal("accepted a short key")
	}
}
