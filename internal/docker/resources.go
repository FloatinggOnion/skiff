package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"
)

// DBRunSpec runs a managed resource (a database), deliberately unlabeled with skiff=1 so the app reaper leaves it alone.
type DBRunSpec struct {
	Name       string
	Image      string
	Network    string
	Volume     string // named volume for persistence
	MountAt    string // where the volume mounts inside the container
	Env        map[string]string
	Cmd        []string          // optional command/args after the image
	Labels     map[string]string // ownership + kind labels
	Port       int               // container port (published on the host when Publish is set)
	Publish    bool              // expose Port on 0.0.0.0 for external access
	Entrypoint string            // optional --entrypoint override (e.g. "sh" to fix TLS cert perms)
	Binds      []string          // extra bind mounts, "host:container[:ro]" (e.g. the TLS cert dir)
}

// RunDatabase (re)creates a managed database container, returning the published host port when Publish is set (0 otherwise); reusing the same name + volume preserves the data.
func (e *Engine) RunDatabase(s DBRunSpec) (int, error) {
	_ = e.command("rm", "-f", s.Name).Run()
	args := []string{"run", "-d", "--name", s.Name, "--restart", "unless-stopped"}
	if s.Entrypoint != "" {
		args = append(args, "--entrypoint", s.Entrypoint)
	}
	for _, k := range sortedKeys(s.Labels) {
		args = append(args, "--label", k+"="+s.Labels[k])
	}
	if s.Network != "" {
		args = append(args, "--network", s.Network)
	}
	if s.Volume != "" && s.MountAt != "" {
		args = append(args, "-v", s.Volume+":"+s.MountAt)
	}
	for _, b := range s.Binds {
		args = append(args, "-v", b)
	}
	if s.Publish && s.Port > 0 {
		args = append(args, "-p", fmt.Sprintf("0.0.0.0::%d", s.Port))
	}
	for _, k := range sortedKeys(s.Env) {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, s.Env[k]))
	}
	args = append(args, s.Image)
	args = append(args, s.Cmd...)
	if out, err := e.command(args...).CombinedOutput(); err != nil {
		return 0, fmt.Errorf("docker run failed: %s", firstLine(out))
	}
	if s.Publish && s.Port > 0 {
		return e.HostPort(s.Name, s.Port)
	}
	return 0, nil
}

func (e *Engine) RemoveVolume(name string) error {
	return e.command("volume", "rm", "-f", name).Run()
}

func (e *Engine) PullImage(image string) error {
	if out, err := e.command("pull", image).CombinedOutput(); err != nil {
		return fmt.Errorf("docker pull failed: %s", firstLine(out))
	}
	return nil
}

// RunOnce runs a throwaway container executing a shell command (sh -c), for release commands (migrations) and scheduled jobs.
func (e *Engine) RunOnce(ctx context.Context, image string, env map[string]string, network string, volumes []Mount, cmd string) (string, error) {
	args := []string{"run", "--rm"}
	if network != "" {
		args = append(args, "--network", network)
	}
	args = append(args, mountArgs(volumes)...)
	for _, k := range sortedKeys(env) {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, env[k]))
	}
	args = append(args, image, "sh", "-c", cmd)
	out, err := e.contextCommand(ctx, args...).CombinedOutput()
	return string(out), err
}

// Exec runs a command inside a container, wiring stdin/stdout to the given streams — used to dump a database and pipe a dump back in.
func (e *Engine) Exec(ctx context.Context, container string, cmd []string, stdin io.Reader, stdout io.Writer) error {
	args := append([]string{"exec", "-i", container}, cmd...)
	c := e.contextCommand(ctx, args...)
	c.Stdin = stdin
	c.Stdout = stdout
	var errb bytes.Buffer
	c.Stderr = &errb
	if err := c.Run(); err != nil {
		if msg := firstLine(errb.Bytes()); msg != "" {
			return fmt.Errorf("%s", msg)
		}
		return err
	}
	return nil
}
