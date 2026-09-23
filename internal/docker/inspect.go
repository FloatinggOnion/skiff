package docker

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func (e *Engine) HostPort(name string, containerPort int) (int, error) {
	out, err := e.command("port", name, strconv.Itoa(containerPort)).Output()
	if err != nil {
		return 0, fmt.Errorf("reading published port: %w", err)
	}
	line := firstLine(out) // e.g. "127.0.0.1:49153"
	i := strings.LastIndexByte(line, ':')
	if i < 0 {
		return 0, fmt.Errorf("unexpected `docker port` output: %q", line)
	}
	return strconv.Atoi(strings.TrimSpace(line[i+1:]))
}

func (e *Engine) State(container string) string {
	out, err := e.command("inspect", "-f", "{{.State.Status}}", container).Output()
	if err != nil {
		return "missing"
	}
	return strings.TrimSpace(string(out))
}

func (e *Engine) Containers() ([]string, error) {
	out, err := e.command("ps", "-a", "--filter", "label=skiff=1", "--format", "{{.Names}}").Output()
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// AppContainers lists every container (running or not) for an app, so all stale versions get retired, not just the registry's last-recorded one.
func (e *Engine) AppContainers(app string) []string {
	out, err := e.command("ps", "-a", "--filter", "label=skiff.app="+app, "--format", "{{.Names}}").Output()
	if err != nil {
		return nil
	}
	return splitLines(out)
}

type ContainerInfo struct {
	Name    string
	Created time.Time
	App     string // skiff.app label; "" for containers not tied to an app
	Port    int    // skiff.port label
	Running bool
}

func (e *Engine) SkiffContainers() []ContainerInfo {
	out, err := e.command("ps", "-a", "--filter", "label=skiff=1", "--format",
		`{{.Names}}|{{.CreatedAt}}|{{.Label "skiff.app"}}|{{.Label "skiff.port"}}|{{.State}}`).Output()
	if err != nil {
		return nil
	}
	return parseSkiffContainers(out)
}

func parseSkiffContainers(out []byte) []ContainerInfo {
	var cs []ContainerInfo
	for _, line := range splitLines(out) {
		f := strings.Split(line, "|")
		if len(f) < 2 || f[0] == "" {
			continue
		}
		c := ContainerInfo{Name: f[0]}
		c.Created, _ = time.Parse("2006-01-02 15:04:05 -0700 MST", strings.TrimSpace(f[1]))
		if len(f) >= 5 {
			c.App = strings.TrimSpace(f[2])
			c.Port, _ = strconv.Atoi(strings.TrimSpace(f[3]))
			c.Running = strings.TrimSpace(f[4]) == "running"
		}
		cs = append(cs, c)
	}
	return cs
}

func (e *Engine) Routes() ([]Route, error) {
	out, err := e.command("ps", "--filter", "label=skiff.app", "--format", `{{.Label "skiff.app"}} {{.Label "skiff.port"}} {{.Names}}`).Output()
	if err != nil {
		return nil, err
	}
	var routes []Route
	for _, line := range splitLines(out) {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		cport, err := strconv.Atoi(f[1])
		if err != nil {
			continue
		}
		hp, err := e.HostPort(f[2], cport)
		if err != nil {
			continue
		}
		routes = append(routes, Route{App: f[0], HostPort: hp})
	}
	return routes, nil
}

// AppStates maps each skiff app to its container state; running wins when an app has more than one container (e.g. mid-rollout).
func (e *Engine) AppStates() (map[string]string, error) {
	out, err := e.command("ps", "-a", "--filter", "label=skiff.app", "--format", `{{.Label "skiff.app"}}|{{.State}}`).Output()
	if err != nil {
		return nil, err
	}
	states := map[string]string{}
	for _, line := range splitLines(out) {
		app, state, ok := strings.Cut(line, "|")
		if !ok || app == "" {
			continue
		}
		if states[app] != "running" {
			states[app] = state
		}
	}
	return states, nil
}
