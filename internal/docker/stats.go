package docker

import (
	"strconv"
	"strings"
)

type ContainerResource struct {
	App       string
	Container string
	CPUPerc   float64 // percent of one core; can exceed 100 on multi-core
	MemBytes  int64
	MemLimit  int64
	Restarts  int
}

func (e *Engine) AppResourceStats() ([]ContainerResource, error) {
	appOf, err := e.AppContainerKeys()
	if err != nil {
		return nil, err
	}
	return e.StatsFor(appOf)
}

// AppContainerKeys maps each running app container to its app.
func (e *Engine) AppContainerKeys() (map[string]string, error) {
	out, err := e.command("ps", "--filter", "label=skiff.app", "--format", `{{.Names}} {{.Label "skiff.app"}}`).Output()
	if err != nil {
		return nil, err
	}
	appOf := map[string]string{}
	for _, line := range splitLines(out) {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		appOf[f[0]] = f[1]
	}
	return appOf, nil
}

// StatsFor samples the named containers once, reporting each under the key it maps to.
func (e *Engine) StatsFor(appOf map[string]string) ([]ContainerResource, error) {
	names := make([]string, 0, len(appOf))
	for n := range appOf {
		names = append(names, n)
	}
	if len(names) == 0 {
		return nil, nil
	}

	stats, err := e.command(append([]string{"stats", "--no-stream", "--format", "{{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}"}, names...)...).Output()
	if err != nil {
		return nil, err
	}
	restarts := e.restartCounts(names)

	var res []ContainerResource
	for _, line := range splitLines(stats) {
		f := strings.Split(line, "\t")
		if len(f) < 3 {
			continue
		}
		name := strings.TrimSpace(f[0])
		app := appOf[name]
		if app == "" {
			continue
		}
		var used, limit int64
		if u, l, ok := strings.Cut(f[2], "/"); ok {
			used, limit = parseSize(u), parseSize(l)
		}
		res = append(res, ContainerResource{
			App:       app,
			Container: name,
			CPUPerc:   parsePercent(f[1]),
			MemBytes:  used,
			MemLimit:  limit,
			Restarts:  restarts[name],
		})
	}
	return res, nil
}

func (e *Engine) restartCounts(names []string) map[string]int {
	counts := map[string]int{}
	out, err := e.command(append([]string{"inspect", "--format", "{{.Name}}\t{{.RestartCount}}"}, names...)...).Output()
	if err != nil {
		return counts
	}
	for _, line := range splitLines(out) {
		name, n, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		c, _ := strconv.Atoi(strings.TrimSpace(n))
		counts[strings.TrimPrefix(strings.TrimSpace(name), "/")] = c
	}
	return counts
}

func parsePercent(s string) float64 {
	s = strings.TrimSuffix(strings.TrimSpace(s), "%")
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func parseSize(s string) int64 {
	s = strings.TrimSpace(s)
	i := 0
	for i < len(s) && (s[i] == '.' || (s[i] >= '0' && s[i] <= '9')) {
		i++
	}
	num, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0
	}
	unit := strings.ToLower(strings.TrimSpace(s[i:]))
	mult := 1.0
	switch {
	case strings.HasPrefix(unit, "ki"), unit == "kb", unit == "k":
		mult = 1 << 10
	case strings.HasPrefix(unit, "mi"), unit == "mb", unit == "m":
		mult = 1 << 20
	case strings.HasPrefix(unit, "gi"), unit == "gb", unit == "g":
		mult = 1 << 30
	case strings.HasPrefix(unit, "ti"), unit == "tb", unit == "t":
		mult = 1 << 40
	}
	return int64(num * mult)
}

// WorkerContainerKeys maps each running worker container to its app.
func (e *Engine) WorkerContainerKeys() (map[string]string, error) {
	out, err := e.command("ps", "--filter", "label=skiff.worker.app", "--format", `{{.Names}} {{.Label "skiff.worker.app"}}`).Output()
	if err != nil {
		return nil, err
	}
	keys := map[string]string{}
	for _, line := range splitLines(out) {
		if f := strings.Fields(line); len(f) == 2 {
			keys[f[0]] = f[1]
		}
	}
	return keys, nil
}
