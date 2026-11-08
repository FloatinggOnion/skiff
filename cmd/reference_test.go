package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestCLIReference keeps the docs' CLI page in step with the commands.
func TestCLIReference(t *testing.T) {
	var b bytes.Buffer
	WriteReference(&b)
	path := filepath.Join("..", "web", "docs", "content", "docs", "cli.mdx")
	if os.Getenv("SKIFF_UPDATE_DOCS") == "1" {
		if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, b.Bytes()) {
		t.Fatal("web/docs/content/docs/cli.mdx is out of date: run SKIFF_UPDATE_DOCS=1 go test ./cmd -run TestCLIReference")
	}
}
