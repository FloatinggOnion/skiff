package panel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mkdirs(t *testing.T, root string, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCheckSymlinks(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		build func(root string) error
		bad   bool
	}{
		{"link inside the repo", func(root string) error {
			mkdirs(t, root, "app/src")
			if err := os.WriteFile(filepath.Join(root, "app/src/a.txt"), nil, 0o644); err != nil {
				return err
			}
			return os.Symlink("src/a.txt", filepath.Join(root, "app/b.txt"))
		}, false},
		{"relative link to a sibling dir", func(root string) error {
			mkdirs(t, root, "shared", "svc")
			return os.Symlink("../shared", filepath.Join(root, "svc/shared"))
		}, false},
		{"absolute link", func(root string) error { return os.Symlink(outside, filepath.Join(root, "x")) }, true},
		{"relative link out", func(root string) error {
			mkdirs(t, root, "a")
			return os.Symlink("../../../../../../../../etc", filepath.Join(root, "a/x"))
		}, true},
		{"chain through the repo", func(root string) error {
			if err := os.Symlink(outside, filepath.Join(root, "hop")); err != nil {
				return err
			}
			return os.Symlink("hop", filepath.Join(root, "x"))
		}, true},
		{"file link out", func(root string) error {
			return os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, ".env"))
		}, true},
		{"dangling inside", func(root string) error { return os.Symlink("nowhere", filepath.Join(root, "x")) }, false},
		{"link inside .git is ignored", func(root string) error {
			mkdirs(t, root, ".git")
			return os.Symlink(outside, filepath.Join(root, ".git/hook"))
		}, false},
	}
	for _, c := range cases {
		root := t.TempDir()
		if err := c.build(root); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		err := checkSymlinks(root)
		if c.bad && (err == nil || !strings.Contains(err.Error(), "points outside")) {
			t.Errorf("%s: want a refusal, got %v", c.name, err)
		}
		if !c.bad && err != nil {
			t.Errorf("%s: unexpected refusal: %v", c.name, err)
		}
	}
}

func TestInsideCheckout(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	mkdirs(t, root, "sub")
	if err := os.Symlink(outside, filepath.Join(root, "esc")); err != nil {
		t.Fatal(err)
	}
	if !insideCheckout(root, filepath.Join(root, "sub")) {
		t.Error("a real subfolder should be inside")
	}
	if insideCheckout(root, filepath.Join(root, "esc")) {
		t.Error("a link to elsewhere should not be inside")
	}
}
