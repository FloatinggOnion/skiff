package panel

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nuelScript/skiff/internal/gate"
)

func TestWriteGateFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_ = os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".skiff"), 0o755)
	openTestDB(t)
	for _, s := range []Source{
		{App: "shop", Repo: "acme/shop", Branch: "main"},
		{App: "shop-feat-x", Repo: "acme/shop", Branch: "feat/x", Parent: "shop"},
		{App: "blog", Repo: "acme/blog", Branch: "main"},
		{App: "blog-feat-y", Repo: "acme/blog", Branch: "feat/y", Parent: "blog"},
	} {
		if err := putSource(s); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := sqlDB.Exec(`INSERT INTO preview_passwords(app,hash) VALUES('shop','h1')`); err != nil {
		t.Fatal(err)
	}
	if err := writeGateFile(); err != nil {
		t.Fatal(err)
	}
	got := gate.Load(gateFile())
	if len(got) != 1 || got["shop-feat-x"] != "h1" {
		t.Fatalf("only shop's preview should be protected, got %v", got)
	}

	deleteSource("shop")
	if previewPasswordHash("shop") != "" {
		t.Fatal("deleting the project left its preview password behind")
	}
}
