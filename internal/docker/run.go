package docker

import (
	"fmt"
)

type RunSpec struct {
	Name          string
	App           string // app name, for the skiff.app route label
	Image         string
	ContainerPort int
	Memory        string // optional, e.g. "512m"
	CPU           string // optional, e.g. "0.5"
	Env           map[string]string
	Public        bool   // publish on all interfaces instead of 127.0.0.1
	Network       string // optional docker network to join (for reaching managed resources by name)
	Labels        map[string]string
	Volumes       []Mount
}

type Route struct {
	App      string
	HostPort int
}

func (e *Engine) Run(s RunSpec) (int, error) {
	defer invalidateStates()
	_ = e.command("rm", "-f", s.Name).Run()

	bind := "127.0.0.1"
	if s.Public {
		bind = "0.0.0.0"
	}
	args := []string{"run", "-d",
		"--name", s.Name,
		"--restart", "unless-stopped",
		"--label", "skiff=1",
		"-e", fmt.Sprintf("PORT=%d", s.ContainerPort),
		"-p", fmt.Sprintf("%s::%d", bind, s.ContainerPort),
	}
	if s.App != "" {
		args = append(args, "--label", "skiff.app="+s.App, "--label", fmt.Sprintf("skiff.port=%d", s.ContainerPort))
	}
	for _, k := range sortedKeys(s.Labels) {
		if s.Labels[k] != "" {
			args = append(args, "--label", k+"="+s.Labels[k])
		}
	}
	if s.Network != "" {
		args = append(args, "--network", s.Network)
	}
	if s.Memory != "" {
		args = append(args, "--memory", s.Memory)
	}
	if s.CPU != "" {
		args = append(args, "--cpus", s.CPU)
	}
	args = append(args, mountArgs(s.Volumes)...)
	for _, k := range sortedKeys(s.Env) {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, s.Env[k]))
	}
	args = append(args, s.Image)

	out, err := e.command(args...).CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker run failed: %s", firstLine(out))
	}
	return e.HostPort(s.Name, s.ContainerPort)
}

// WorkerSpec runs a long-lived background process with no published port or routing labels, so the edge router never targets it.
type WorkerSpec struct {
	Name    string
	App     string
	Image   string
	Command string // run via sh -c
	Env     map[string]string
	Network string
	Volumes []Mount
}

func (e *Engine) RunWorker(s WorkerSpec) error {
	_ = e.command("rm", "-f", s.Name).Run()
	args := []string{"run", "-d", "--name", s.Name, "--restart", "unless-stopped",
		"--label", "skiff.kind=worker", "--label", "skiff.worker.app=" + s.App}
	if s.Network != "" {
		args = append(args, "--network", s.Network)
	}
	args = append(args, mountArgs(s.Volumes)...)
	for _, k := range sortedKeys(s.Env) {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, s.Env[k]))
	}
	args = append(args, s.Image, "sh", "-c", s.Command)
	if out, err := e.command(args...).CombinedOutput(); err != nil {
		return cmdErr(out, err)
	}
	return nil
}

// WorkerContainers lists an app's worker containers (running or not); all of them when app is "".
func (e *Engine) WorkerContainers(app string) []string {
	filter := "label=skiff.kind=worker"
	if app != "" {
		filter = "label=skiff.worker.app=" + app
	}
	out, err := e.command("ps", "-a", "--filter", filter, "--format", "{{.Names}}").Output()
	if err != nil {
		return nil
	}
	return splitLines(out)
}

func (e *Engine) EnsureNetwork(name string) error {
	if e.command("network", "inspect", name).Run() == nil {
		return nil
	}
	if out, err := e.command("network", "create", name).CombinedOutput(); err != nil {
		return fmt.Errorf("network create failed: %s", firstLine(out))
	}
	return nil
}

// ConnectNetwork attaches a container to an additional network; an already-connected container returns a benign error.
func (e *Engine) ConnectNetwork(network, container string) error {
	out, err := e.command("network", "connect", network, container).CombinedOutput()
	if err != nil {
		return cmdErr(out, err)
	}
	return nil
}
