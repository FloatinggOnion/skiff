package docker

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// BuildFromDockerfile builds contextDir into tag. With pull, every base image comes from its registry, so a build can't use images other apps left on this machine.
func (e *Engine) BuildFromDockerfile(ctx context.Context, tag, dockerfile, contextDir string, pull bool, out io.Writer) error {
	defer ensureDockerignore(contextDir)()

	args := []string{"build", "-t", tag, "-f", "-"}
	if pull {
		args = append(args, "--pull")
	}
	cmd := e.contextCommand(ctx, append(args, contextDir)...)
	cmd.Stdin = strings.NewReader(dockerfile)
	cmd.Stdout = out
	cmd.Stderr = out
	return cmd.Run()
}

// skiff.toml holds an app's secrets, so it never goes into the image, even when a Dockerfile copies everything. The checkout's .git can hold the credentials it was cloned with.
const defaultDockerignore = "node_modules\n.git\n.skiff\n.env\n*.log\nskiff.toml\n"

// alwaysIgnored comes after an app's own rules, which can't re-include these.
const alwaysIgnored = "\n.git\nskiff.toml\n"

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
	if err := os.WriteFile(p, append(append([]byte{}, existing...), alwaysIgnored...), 0o644); err != nil {
		return func() {}
	}
	return func() { _ = os.WriteFile(p, existing, 0o644) }
}
