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
	Aliases    []string          // extra names on the network, such as a Compose service name
	Binds      []string          // extra bind mounts, "host:container[:ro]" (e.g. the TLS cert dir)
	Memory     string            // optional memory limit; otherwise the safety cap
}

// RunDatabase (re)creates a managed database container, returning the published host port when Publish is set (0 otherwise); reusing the same name + volume preserves the data.
func (e *Engine) RunDatabase(s DBRunSpec) (int, error) {
	defer invalidateStates()
	_ = e.command("rm", "-f", s.Name).Run()
	args := []string{"run", "-d", "--name", s.Name, "--restart", "unless-stopped"}
	if s.Entrypoint != "" {
		args = append(args, "--entrypoint", s.Entrypoint)
	}
	args = append(args, e.limitArgs()...)
	args = append(args, "--memory", e.memoryOr(s.Memory))
	for _, k := range sortedKeys(s.Labels) {
		args = append(args, "--label", k+"="+s.Labels[k])
	}
	if s.Network != "" {
		args = append(args, "--network", s.Network)
		for _, a := range s.Aliases {
			args = append(args, "--network-alias", a)
		}
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

// ContainersLabelled names every container with a label, running or not.
func (e *Engine) ContainersLabelled(label string) []string {
	out, err := e.command("ps", "-a", "--filter", "label="+label, "--format", "{{.Names}}").Output()
	if err != nil {
		return nil
	}
	return splitLines(out)
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

// RunOnce runs a throwaway container executing a shell command (sh -c), for release commands (migrations) and scheduled jobs, with the same settings as the app it belongs to.
func (e *Engine) RunOnce(ctx context.Context, image, cmd string, s Settings) (string, error) {
	args := append([]string{"run", "--rm"}, e.settingsArgs(s)...)
	args = append(args, image, "sh", "-c", cmd)
	out, err := e.contextCommand(ctx, args...).CombinedOutput()
	return string(out), err
}

// RunIO runs a throwaway container with its streams attached and no memory limit, for one-off work on a volume whose size isn't known in advance.
func (e *Engine) RunIO(ctx context.Context, image string, volumes []Mount, cmd string, stdin io.Reader, stdout io.Writer) error {
	args := []string{"run", "--rm", "-i"}
	args = append(args, mountArgs(volumes)...)
	args = append(args, image, "sh", "-c", cmd)
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
