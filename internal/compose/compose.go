// Package compose reads a Docker Compose file and works out what Skiff would create from it.
package compose

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// FileNames are the names Compose itself looks for, in its order.
var FileNames = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}

type File struct {
	Services map[string]Service
	// Order keeps services in the order they're written, for stable output.
	Order []string
	// Unset lists variables the file uses that have no value.
	Unset []string
	// Extra lists keys Skiff doesn't read, as "service: key".
	Extra map[string][]string
}

type Service struct {
	Image       string
	Build       *Build
	Command     []string
	Environment map[string]string
	EnvFiles    []string
	Ports       []Port
	Expose      []int
	Volumes     []Mount
	DependsOn   []string
	Replicas    int
	Memory      string
	CPUs        string
	Healthcheck []string
}

type Build struct{ Context, Dockerfile string }

type Port struct {
	Published string
	Target    int
}

// Mount is a volume entry: a named volume, an anonymous one (empty Source) or a bind mount.
type Mount struct {
	Source, Target string
	Bind           bool
}

type rawService struct {
	Image       string     `yaml:"image"`
	Build       rawBuild   `yaml:"build"`
	Command     stringList `yaml:"command"`
	Environment envMap     `yaml:"environment"`
	EnvFile     envFiles   `yaml:"env_file"`
	Ports       []rawPort  `yaml:"ports"`
	Expose      []string   `yaml:"expose"`
	Volumes     []rawMount `yaml:"volumes"`
	DependsOn   dependsOn  `yaml:"depends_on"`
	MemLimit    string     `yaml:"mem_limit"`
	CPUs        string     `yaml:"cpus"`
	Deploy      struct {
		Replicas  *int `yaml:"replicas"`
		Resources struct {
			Limits struct {
				Memory string `yaml:"memory"`
				CPUs   string `yaml:"cpus"`
			} `yaml:"limits"`
		} `yaml:"resources"`
	} `yaml:"deploy"`
	Healthcheck struct {
		Test stringList `yaml:"test"`
	} `yaml:"healthcheck"`
}

var serviceKeys = map[string]bool{
	"image": true, "build": true, "command": true, "environment": true, "env_file": true, "ports": true,
	"expose": true, "volumes": true, "depends_on": true, "mem_limit": true, "cpus": true, "deploy": true,
	"healthcheck": true, "<<": true,
}

// Parse reads a Compose file, filling in ${VAR} references from lookup.
func Parse(data []byte, lookup func(string) (string, bool)) (*File, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("reading the Compose file: %w", err)
	}
	if len(doc.Content) == 0 {
		return nil, errors.New("the Compose file is empty")
	}
	f := &File{Services: map[string]Service{}, Extra: map[string][]string{}}
	if err := interpolate(&doc, lookup, f, map[*yaml.Node]bool{}); err != nil {
		return nil, err
	}
	services := mapValue(doc.Content[0], "services")
	if services == nil || services.Kind != yaml.MappingNode || len(services.Content) == 0 {
		return nil, errors.New("the Compose file has no services")
	}
	for i := 0; i+1 < len(services.Content); i += 2 {
		name, node := services.Content[i].Value, services.Content[i+1]
		var raw rawService
		if err := node.Decode(&raw); err != nil {
			return nil, fmt.Errorf("service %s: %w", name, err)
		}
		s := Service{
			Image: raw.Image, Command: raw.Command, Environment: raw.Environment, EnvFiles: raw.EnvFile,
			DependsOn: raw.DependsOn, Memory: raw.MemLimit, CPUs: raw.CPUs, Healthcheck: raw.Healthcheck.Test, Replicas: 1,
		}
		if raw.Build.Context != "" {
			b := Build(raw.Build)
			s.Build = &b
		}
		if raw.Deploy.Replicas != nil {
			s.Replicas = *raw.Deploy.Replicas
		}
		if m := raw.Deploy.Resources.Limits.Memory; m != "" {
			s.Memory = m
		}
		if c := raw.Deploy.Resources.Limits.CPUs; c != "" {
			s.CPUs = c
		}
		for _, p := range raw.Ports {
			s.Ports = append(s.Ports, Port(p))
		}
		for _, e := range raw.Expose {
			if n, err := strconv.Atoi(strings.Split(e, "/")[0]); err == nil {
				s.Expose = append(s.Expose, n)
			}
		}
		for _, m := range raw.Volumes {
			s.Volumes = append(s.Volumes, Mount(m))
		}
		f.Services[name] = s
		f.Order = append(f.Order, name)
		if node.Kind == yaml.MappingNode {
			for j := 0; j+1 < len(node.Content); j += 2 {
				if k := node.Content[j].Value; !serviceKeys[k] && !strings.HasPrefix(k, "x-") {
					f.Extra[name] = append(f.Extra[name], k)
				}
			}
		}
	}
	return f, nil
}

func mapValue(n *yaml.Node, key string) *yaml.Node {
	if n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

var varPattern = regexp.MustCompile(`\$\$|\$\{([A-Za-z_][A-Za-z0-9_]*)(?:(:?[-?+])([^}]*))?\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

func interpolate(n *yaml.Node, lookup func(string) (string, bool), f *File, seen map[*yaml.Node]bool) error {
	if seen[n] {
		return nil
	}
	seen[n] = true
	switch n.Kind {
	case yaml.MappingNode:
		for i := 1; i < len(n.Content); i += 2 {
			if err := interpolate(n.Content[i], lookup, f, seen); err != nil {
				return err
			}
		}
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, c := range n.Content {
			if err := interpolate(c, lookup, f, seen); err != nil {
				return err
			}
		}
	case yaml.ScalarNode:
		var failed error
		n.Value = varPattern.ReplaceAllStringFunc(n.Value, func(m string) string {
			if m == "$$" {
				return "$"
			}
			g := varPattern.FindStringSubmatch(m)
			name, op, arg := g[1], g[2], g[3]
			if name == "" {
				name = g[4]
			}
			v, set := lookup(name)
			usable := set && (v != "" || !strings.HasPrefix(op, ":"))
			switch strings.TrimPrefix(op, ":") {
			case "-":
				if !usable {
					return arg
				}
			case "?":
				if !usable {
					failed = fmt.Errorf("%s must be set: %s", name, arg)
				}
			case "+":
				if usable {
					return arg
				}
				return ""
			default:
				if !set {
					f.Unset = append(f.Unset, name)
				}
			}
			return v
		})
		return failed
	}
	return nil
}

type stringList []string

func (s *stringList) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.SequenceNode {
		var l []string
		if err := n.Decode(&l); err != nil {
			return err
		}
		*s = l
		return nil
	}
	if n.Value != "" {
		*s = shellWords(n.Value)
	}
	return nil
}

// shellWords splits a command the way Compose does for its string form, honouring quotes.
func shellWords(c string) []string {
	var out []string
	var b strings.Builder
	var quote rune
	started := false
	for _, r := range c {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote, started = r, true
		case r == ' ' || r == '\t' || r == '\n':
			if started {
				out = append(out, b.String())
				b.Reset()
				started = false
			}
		default:
			b.WriteRune(r)
			started = true
		}
	}
	if started {
		out = append(out, b.String())
	}
	return out
}

type envMap map[string]string

func (e *envMap) UnmarshalYAML(n *yaml.Node) error {
	out := map[string]string{}
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value != "<<" {
				out[n.Content[i].Value] = n.Content[i+1].Value
			}
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			k, v, _ := strings.Cut(c.Value, "=")
			out[k] = v
		}
	}
	*e = out
	return nil
}

type envFiles []string

func (e *envFiles) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		*e = []string{n.Value}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			if c.Kind == yaml.ScalarNode {
				*e = append(*e, c.Value)
			} else if p := mapValue(c, "path"); p != nil {
				*e = append(*e, p.Value)
			}
		}
	}
	return nil
}

type rawBuild Build

func (b *rawBuild) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		b.Context = n.Value
		return nil
	}
	b.Context = "."
	if c := mapValue(n, "context"); c != nil {
		b.Context = c.Value
	}
	if d := mapValue(n, "dockerfile"); d != nil {
		b.Dockerfile = d.Value
	}
	return nil
}

type rawPort Port

func (p *rawPort) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.MappingNode {
		if t := mapValue(n, "target"); t != nil {
			p.Target, _ = strconv.Atoi(t.Value)
		}
		if pub := mapValue(n, "published"); pub != nil {
			p.Published = pub.Value
		}
		return nil
	}
	spec := strings.Split(n.Value, "/")[0]
	parts := strings.Split(spec, ":")
	target, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return fmt.Errorf("port %q: only single ports are supported", n.Value)
	}
	p.Target = target
	if len(parts) > 1 {
		p.Published = parts[len(parts)-2]
	}
	return nil
}

type rawMount Mount

func (m *rawMount) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.MappingNode {
		kind := ""
		if t := mapValue(n, "type"); t != nil {
			kind = t.Value
		}
		if s := mapValue(n, "source"); s != nil {
			m.Source = s.Value
		}
		if t := mapValue(n, "target"); t != nil {
			m.Target = t.Value
		}
		m.Bind = kind == "bind"
		return nil
	}
	parts := strings.Split(n.Value, ":")
	if len(parts) == 1 {
		m.Target = parts[0]
		return nil
	}
	m.Source, m.Target = parts[0], parts[1]
	m.Bind = strings.ContainsAny(m.Source, "/.~$")
	return nil
}

type dependsOn []string

func (d *dependsOn) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.SequenceNode:
		for _, c := range n.Content {
			*d = append(*d, c.Value)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			*d = append(*d, n.Content[i].Value)
		}
	}
	return nil
}
