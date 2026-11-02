package docker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// RegistryAuth signs in to the image's registry for a single pull.
type RegistryAuth struct {
	Host     string
	Username string
	Password string
}

// Pull fetches an image, streaming docker's progress to out. With auth, the pull uses a throwaway docker config so the credentials are never written to the user's own.
func (e *Engine) Pull(ctx context.Context, image string, auth *RegistryAuth, out io.Writer) error {
	cmd := e.contextCommand(ctx, "pull", image)
	if auth != nil && auth.Username != "" {
		dir, err := os.MkdirTemp("", "skiff-pull-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		if err := os.WriteFile(filepath.Join(dir, "config.json"), dockerAuthConfig(*auth), 0o600); err != nil {
			return err
		}
		if cmd.Env == nil {
			cmd.Env = os.Environ()
		}
		cmd.Env = append(cmd.Env, "DOCKER_CONFIG="+dir)
	}
	cmd.Stdout, cmd.Stderr = out, out
	return cmd.Run()
}

func dockerAuthConfig(a RegistryAuth) []byte {
	host := a.Host
	if host == "" || host == "docker.io" {
		host = "https://index.docker.io/v1/"
	}
	cfg := map[string]any{"auths": map[string]any{
		host: map[string]string{"auth": base64.StdEncoding.EncodeToString([]byte(a.Username + ":" + a.Password))},
	}}
	b, _ := json.Marshal(cfg)
	return b
}

// ImagePort is the lowest TCP port the image declares with EXPOSE, or 0 when it declares none.
func (e *Engine) ImagePort(image string) int {
	out, err := e.command("image", "inspect", "--format", "{{json .Config.ExposedPorts}}", image).Output()
	if err != nil {
		return 0
	}
	return parseExposedPort(out)
}

func parseExposedPort(out []byte) int {
	var ports map[string]any
	if json.Unmarshal(out, &ports) != nil {
		return 0
	}
	var tcp []int
	for k := range ports {
		num, proto, _ := strings.Cut(k, "/")
		if proto != "" && proto != "tcp" {
			continue
		}
		if n, err := strconv.Atoi(num); err == nil && n > 0 {
			tcp = append(tcp, n)
		}
	}
	if len(tcp) == 0 {
		return 0
	}
	sort.Ints(tcp)
	return tcp[0]
}

// ImageDigest is the registry digest a pulled image came from, if it has one.
func (e *Engine) ImageDigest(image string) string {
	out, err := e.command("image", "inspect", "--format", "{{range .RepoDigests}}{{println .}}{{end}}", image).Output()
	if err != nil {
		return ""
	}
	for _, ln := range splitLines(out) {
		if _, d, ok := strings.Cut(ln, "@"); ok {
			return d
		}
	}
	return ""
}
