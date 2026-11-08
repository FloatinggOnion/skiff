package panel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStackAppTouched(t *testing.T) {
	built := Source{RootDir: "api", MountedFiles: `[{"from":"deploy/app.conf","to":"/etc/app.conf"}]`}
	image := Source{Image: "x", MountedFiles: `[{"from":"deploy/site","to":"/www"}]`}
	for _, tc := range []struct {
		name  string
		src   Source
		paths []string
		want  bool
	}{
		{"its own folder", built, []string{"api/main.go"}, true},
		{"a file it mounts", built, []string{"deploy/app.conf"}, true},
		{"something else", built, []string{"docs/readme.md"}, false},
		{"an image with a mounted folder", image, []string{"deploy/site/index.html"}, true},
		{"an image and something else", image, []string{"api/main.go"}, false},
		{"a push without a file list", image, nil, true},
	} {
		if got := stackAppTouched(tc.src, tc.paths); got != tc.want {
			t.Errorf("%s: got %v", tc.name, got)
		}
	}
}

func TestPrepareMountedFiles(t *testing.T) {
	work := t.TempDir()
	write := func(path, body string) {
		t.Helper()
		full := filepath.Join(work, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("conf/app.conf", "x")
	write("site/index.html", "x")
	write("site/.git/config", "token")
	write("site/skiff.toml", "secret")
	write("api/Dockerfile", "FROM scratch\n")

	src := Source{Image: "nginx:1", MountedFiles: `[{"from":"conf/app.conf","to":"/etc/app conf"},{"from":"site","to":"/www"}]`}
	if err := prepareMountedFiles(&src, work, work); err != nil {
		t.Fatal(err)
	}
	if src.Image != "" || src.Dockerfile != ".skiff-mounts/Dockerfile" {
		t.Errorf("source: %+v", src)
	}
	data, _ := os.ReadFile(filepath.Join(work, src.Dockerfile))
	want := "FROM nginx:1\nCOPY [\".skiff-mounts/0/app.conf\",\"/etc/app conf\"]\nCOPY [\".skiff-mounts/1\",\"/www\"]\n"
	if string(data) != want {
		t.Errorf("Dockerfile:\n%s", data)
	}
	for _, gone := range []string{".git/config", "skiff.toml"} {
		if _, err := os.Stat(filepath.Join(work, ".skiff-mounts/1", gone)); err == nil {
			t.Errorf("%s reached the image", gone)
		}
	}

	built := Source{Service: "api", RootDir: "api", MountedFiles: `[{"from":"conf/app.conf","to":"/app.conf"}]`}
	if err := prepareMountedFiles(&built, work, filepath.Join(work, "api")); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(work, "api", built.Dockerfile))
	if !strings.HasPrefix(string(data), "FROM scratch\n") || !strings.Contains(string(data), `"/app.conf"`) {
		t.Errorf("built Dockerfile:\n%s", data)
	}

	gone := Source{Service: "api", Image: "x", MountedFiles: `[{"from":"nope","to":"/n"}]`}
	if err := prepareMountedFiles(&gone, work, work); err == nil || !strings.Contains(err.Error(), "isn't in the repository") {
		t.Errorf("a removed file: %v", err)
	}
}
