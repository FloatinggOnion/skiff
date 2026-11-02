package docker

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func (e *Engine) BuildFromDockerfile(ctx context.Context, tag, dockerfile, contextDir string, out io.Writer) error {
	defer ensureDockerignore(contextDir)()

	cmd := e.contextCommand(ctx, "build", "-t", tag, "-f", "-", contextDir)
	cmd.Stdin = strings.NewReader(dockerfile)
	cmd.Stdout = out
	cmd.Stderr = out
	return cmd.Run()
}

// skiff.toml holds an app's secrets, so it never goes into the image, even when a Dockerfile copies everything.
const defaultDockerignore = "node_modules\n.git\n.skiff\n.env\n*.log\nskiff.toml\n"

// ensureDockerignore keeps skiff.toml out of the build for the length of it: it writes a default file when the app ships none, or adds the line to the app's own, and its cleanup puts things back as they were.
func ensureDockerignore(dir string) func() {
	p := filepath.Join(dir, ".dockerignore")
	existing, err := os.ReadFile(p)
	if err != nil {
		if err := os.WriteFile(p, []byte(defaultDockerignore), 0o644); err != nil {
			return func() {}
		}
		return func() { os.Remove(p) }
	}
	if err := os.WriteFile(p, append(append([]byte{}, existing...), "\nskiff.toml\n"...), 0o644); err != nil {
		return func() {}
	}
	return func() { _ = os.WriteFile(p, existing, 0o644) }
}
