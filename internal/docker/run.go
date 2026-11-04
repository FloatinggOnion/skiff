package docker

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
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
	Aliases       []string // extra names on the network, such as a Compose service name
	Command       []string // replaces the image's default command
}

type Route struct {
	App       string
	Container string
	HostPort  int
}

// SafetyCapPercent is how much of the server's memory an app with no limit of its own may use: enough for nearly anything, while leaving room for the other apps and for Skiff to keep running if one runs away.
const SafetyCapPercent = 75

var (
	capMu    sync.Mutex
	capCache = map[string]int64{}
)

// memoryOr is the memory limit for a container: its own, or the safety cap.
func (e *Engine) memoryOr(own string) string {
	if own != "" {
		return own
	}
	capMu.Lock()
	defer capMu.Unlock()
	total, ok := capCache[e.host]
	if !ok {
		out, err := e.command("info", "--format", "{{.MemTotal}}").Output()
		if err != nil {
			return "0" // can't tell the size of the server; leave it unlimited
		}
		total, _ = strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
		capCache[e.host] = total
	}
	return strconv.FormatInt(safetyCap(total), 10)
}

func safetyCap(total int64) int64 { return total / 100 * SafetyCapPercent }

const hostPortWait = 10 * time.Second

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
		// A stable name for other services on the network; docker spreads lookups across replicas.
		if s.App != "" {
			args = append(args, "--network-alias", s.App)
		}
		for _, a := range s.Aliases {
			args = append(args, "--network-alias", a)
		}
	}
	args = append(args, "--memory", e.memoryOr(s.Memory))
	if s.CPU != "" {
		args = append(args, "--cpus", s.CPU)
	}
	args = append(args, mountArgs(s.Volumes)...)
	for _, k := range sortedKeys(s.Env) {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, s.Env[k]))
	}
	args = append(args, s.Image)
	args = append(args, s.Command...)

	out, err := e.command(args...).CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker run failed: %s", firstLine(out))
	}
	return e.waitHostPort(s.Name, s.ContainerPort)
}

// waitHostPort allows for an app that exits straight away: a restarting container has no published port until it's running again, and one that crashes once waiting for its database can still come up.
func (e *Engine) waitHostPort(name string, containerPort int) (int, error) {
	port, err := e.HostPort(name, containerPort)
	for deadline := time.Now().Add(hostPortWait); err != nil && time.Now().Before(deadline); {
		time.Sleep(250 * time.Millisecond)
		port, err = e.HostPort(name, containerPort)
	}
	return port, err
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
	Memory  string
	CPU     string
}

func (e *Engine) RunWorker(s WorkerSpec) error {
	_ = e.command("rm", "-f", s.Name).Run()
	args := []string{"run", "-d", "--name", s.Name, "--restart", "unless-stopped",
		"--label", "skiff.kind=worker", "--label", "skiff.worker.app=" + s.App,
		"--memory", e.memoryOr(s.Memory)}
	if s.CPU != "" {
		args = append(args, "--cpus", s.CPU)
	}
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
// RemoveNetwork deletes a network nothing is using any more.
func (e *Engine) RemoveNetwork(name string) error {
	out, err := e.command("network", "rm", name).CombinedOutput()
	if err != nil {
		return cmdErr(out, err)
	}
	return nil
}

func (e *Engine) ConnectNetwork(network, container string) error {
	out, err := e.command("network", "connect", network, container).CombinedOutput()
	if err != nil {
		return cmdErr(out, err)
	}
	return nil
}

func (e *Engine) DisconnectNetwork(network, container string) error {
	out, err := e.command("network", "disconnect", network, container).CombinedOutput()
	if err != nil {
		return cmdErr(out, err)
	}
	return nil
}
