// Package compose reads a Docker Compose file and works out what Skiff would create from it.
package compose

import (
	"errors"
	"fmt"
	"math"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nuelScript/skiff/internal/config"
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
	// Profiled lists services that only run with a Compose profile, so a plain start leaves them out.
	Profiled []string
	// Problems are parts of the file Skiff can't honor, found while reading it.
	Problems []string
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
	Options     config.RunOptions
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
	ReadOnly       bool
	// Need names the secret or config a file mount comes from; it must exist in the repository.
	Need string
	// Content is a config written in the Compose file itself.
	Content *string
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
	Entrypoint      stringList `yaml:"entrypoint"`
	User            string     `yaml:"user"`
	WorkingDir      string     `yaml:"working_dir"`
	Init            bool       `yaml:"init"`
	ReadOnly        bool       `yaml:"read_only"`
	ShmSize         string     `yaml:"shm_size"`
	Tmpfs           stringList `yaml:"tmpfs"`
	Ulimits         rawUlimits `yaml:"ulimits"`
	ExtraHosts      rawHosts   `yaml:"extra_hosts"`
	StopGracePeriod string     `yaml:"stop_grace_period"`
	StopSignal      string     `yaml:"stop_signal"`
	Profiles        []string   `yaml:"profiles"`
	Secrets         []rawRef   `yaml:"secrets"`
	Configs         []rawRef   `yaml:"configs"`
}

// rawRef is a service's use of a secret or config: a name, or a source with an optional target path.
type rawRef struct{ Source, Target string }

func (r *rawRef) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		r.Source = n.Value
		return nil
	}
	var long struct {
		Source string `yaml:"source"`
		Target string `yaml:"target"`
	}
	if err := n.Decode(&long); err != nil {
		return err
	}
	r.Source, r.Target = long.Source, long.Target
	return nil
}

// fileSource is a top-level secret or config.
type fileSource struct {
	File        string  `yaml:"file"`
	Content     *string `yaml:"content"`
	Environment string  `yaml:"environment"`
	External    bool    `yaml:"external"`
}

var serviceKeys = map[string]bool{
	"image": true, "build": true, "command": true, "environment": true, "env_file": true, "ports": true,
	"expose": true, "volumes": true, "depends_on": true, "mem_limit": true, "cpus": true, "deploy": true,
	"healthcheck": true, "<<": true, "entrypoint": true, "user": true, "working_dir": true, "init": true, "read_only": true,
	"shm_size": true, "tmpfs": true, "ulimits": true, "extra_hosts": true, "stop_grace_period": true, "stop_signal": true, "profiles": true,
	"secrets": true, "configs": true,
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
	expandMerges(&doc, map[*yaml.Node]bool{})
	stripMergeTags(&doc, map[*yaml.Node]bool{})
	if err := interpolate(&doc, lookup, f, map[*yaml.Node]bool{}); err != nil {
		return nil, err
	}
	services := mapValue(doc.Content[0], "services")
	if services == nil || services.Kind != yaml.MappingNode || len(services.Content) == 0 {
		return nil, errors.New("the Compose file has no services")
	}
	if err := applyExtends(services); err != nil {
		return nil, err
	}
	var top struct {
		Secrets map[string]fileSource `yaml:"secrets"`
		Configs map[string]fileSource `yaml:"configs"`
	}
	if err := doc.Content[0].Decode(&top); err != nil {
		return nil, fmt.Errorf("reading secrets and configs: %w", err)
	}
	for i := 0; i+1 < len(services.Content); i += 2 {
		name, node := services.Content[i].Value, services.Content[i+1]
		var raw rawService
		if err := node.Decode(&raw); err != nil {
			return nil, fmt.Errorf("service %s: %w", name, err)
		}
		if len(raw.Profiles) > 0 {
			f.Profiled = append(f.Profiled, name)
			continue
		}
		s := Service{
			Image: raw.Image, Command: raw.Command, Environment: raw.Environment, EnvFiles: raw.EnvFile,
			DependsOn: raw.DependsOn, Memory: dockerSize(raw.MemLimit), CPUs: raw.CPUs, Healthcheck: raw.Healthcheck.Test, Replicas: 1,
		}
		s.Options = config.RunOptions{
			Entrypoint: raw.Entrypoint, User: raw.User, WorkingDir: raw.WorkingDir, Init: raw.Init, ReadOnly: raw.ReadOnly,
			ShmSize: dockerSize(raw.ShmSize), Tmpfs: raw.Tmpfs, Ulimits: raw.Ulimits, ExtraHosts: raw.ExtraHosts, StopSignal: raw.StopSignal,
		}
		if raw.StopGracePeriod != "" {
			d, err := time.ParseDuration(raw.StopGracePeriod)
			if err != nil {
				return nil, fmt.Errorf("service %s: stop_grace_period %q should look like 1m30s", name, raw.StopGracePeriod)
			}
			s.Options.StopSeconds = int(d.Seconds())
		}
		if raw.Build.Context != "" {
			b := Build(raw.Build)
			s.Build = &b
		}
		if raw.Deploy.Replicas != nil {
			s.Replicas = *raw.Deploy.Replicas
		}
		if m := raw.Deploy.Resources.Limits.Memory; m != "" {
			s.Memory = dockerSize(m)
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
		for _, r := range raw.Secrets {
			if m, ok := f.fileMount(name, "secret", r, top.Secrets, lookup); ok {
				s.Volumes = append(s.Volumes, m)
			}
		}
		for _, r := range raw.Configs {
			if m, ok := f.fileMount(name, "config", r, top.Configs, lookup); ok {
				s.Volumes = append(s.Volumes, m)
			}
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

// fileMount turns a secret or config into a read-only file mount: secrets land in /run/secrets/<name> and configs at /<name>, unless a target is given. A secret is only taken from a file, since building a value from the environment into an image would leave it there for anyone with the image.
func (f *File) fileMount(service, kind string, r rawRef, sources map[string]fileSource, lookup func(string) (string, bool)) (Mount, bool) {
	src, defined := sources[r.Source]
	target := r.Target
	if target == "" {
		target = r.Source
	}
	if !strings.HasPrefix(target, "/") {
		if kind == "secret" {
			target = "/run/secrets/" + target
		} else {
			target = "/" + target
		}
	}
	m := Mount{Target: cleanTarget(target), ReadOnly: true, Need: kind + " " + r.Source}
	problem := func(why string) (Mount, bool) {
		f.Problems = append(f.Problems, fmt.Sprintf("%s: %s %s %s", service, kind, r.Source, why))
		return Mount{}, false
	}
	switch {
	case !defined:
		return problem("isn't defined at the top of the file")
	case src.External:
		return problem("is external, which Skiff can't read, so give it a file in the repository")
	case src.File != "":
		m.Source, m.Bind = src.File, true
	case src.Content != nil && kind == "config":
		m.Content = src.Content
	case src.Environment != "" && kind == "config":
		v, _ := lookup(src.Environment)
		m.Content = &v
	case src.Environment != "":
		return problem("comes from an environment variable, so give it a file in the repository or set the variable on the service instead")
	default:
		return problem("needs a file")
	}
	return m, true
}

// applyExtends merges each service that extends another in the same file into a copy of that service. Extending a service from another file is left for the plan to refuse.
func applyExtends(services *yaml.Node) error {
	done := map[string]bool{}
	var resolve func(name string, seen []string) error
	resolve = func(name string, seen []string) error {
		if done[name] {
			return nil
		}
		if slices.Contains(seen, name) {
			return fmt.Errorf("extends goes in a circle through %s", strings.Join(append(seen, name), ", "))
		}
		node := mapValue(services, name)
		if node == nil || node.Kind != yaml.MappingNode {
			return nil
		}
		ext := mapValue(node, "extends")
		if ext == nil {
			done[name] = true
			return nil
		}
		base := ext.Value
		if ext.Kind == yaml.MappingNode {
			if mapValue(ext, "file") != nil {
				done[name] = true
				return nil
			}
			if s := mapValue(ext, "service"); s != nil {
				base = s.Value
			}
		}
		if mapValue(services, base) == nil {
			return fmt.Errorf("service %s extends %s, which isn't in this file", name, base)
		}
		if err := resolve(base, append(seen, name)); err != nil {
			return err
		}
		merged := mergeService(mapValue(services, base), node)
		*node = *merged
		done[name] = true
		return nil
	}
	for i := 0; i+1 < len(services.Content); i += 2 {
		if err := resolve(services.Content[i].Value, nil); err != nil {
			return err
		}
	}
	return nil
}

// appendedKeys are lists Compose adds together when a service extends another; other lists replace the base's.
var appendedKeys = map[string]bool{
	"ports": true, "expose": true, "volumes": true, "secrets": true, "configs": true, "dns": true, "dns_search": true,
	"extra_hosts": true, "tmpfs": true, "env_file": true, "cap_add": true, "cap_drop": true, "devices": true,
}

func mergeService(base, child *yaml.Node) *yaml.Node {
	out := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	at := map[string]int{}
	for i := 0; i+1 < len(base.Content); i += 2 {
		if k := base.Content[i].Value; k != "extends" {
			at[k] = len(out.Content)
			out.Content = append(out.Content, base.Content[i], base.Content[i+1])
		}
	}
	for i := 0; i+1 < len(child.Content); i += 2 {
		k, v := child.Content[i].Value, child.Content[i+1]
		if k == "extends" {
			continue
		}
		j, ok := at[k]
		if !ok {
			out.Content = append(out.Content, child.Content[i], v)
			continue
		}
		old := out.Content[j+1]
		if k == "environment" || k == "labels" {
			old, v = asMapping(old), asMapping(v)
		}
		switch {
		case old.Kind == yaml.MappingNode && v.Kind == yaml.MappingNode:
			out.Content[j+1] = mergeService(old, v)
		case old.Kind == yaml.SequenceNode && v.Kind == yaml.SequenceNode && appendedKeys[k]:
			out.Content[j+1] = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: append(slices.Clone(old.Content), v.Content...)}
		default:
			out.Content[j+1] = v
		}
	}
	return out
}

// asMapping turns a KEY=value list into a mapping, so both forms of environment and labels merge the same way.
func asMapping(n *yaml.Node) *yaml.Node {
	if n.Kind != yaml.SequenceNode {
		return n
	}
	out := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, item := range n.Content {
		k, v, _ := strings.Cut(item.Value, "=")
		out.Content = append(out.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k}, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v})
	}
	return out
}

// cleanTarget drops a trailing slash, which Compose accepts in a container path.
func cleanTarget(t string) string {
	if strings.HasPrefix(t, "/") {
		return path.Clean(t)
	}
	return t
}

var sizePattern = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)\s*([kmg]?)b?$`)

// dockerSize rewrites a Compose size such as 1gb, 2G or 134217728 in megabytes, the form Skiff stores; anything else is left for validation to refuse.
func dockerSize(s string) string {
	m := sizePattern.FindStringSubmatch(strings.ToLower(strings.TrimSpace(s)))
	if m == nil {
		return s
	}
	n, _ := strconv.ParseFloat(m[1], 64)
	switch m[2] {
	case "":
		n /= 1 << 20
	case "k":
		n /= 1 << 10
	case "g":
		n *= 1 << 10
	}
	return strconv.FormatFloat(math.Ceil(n), 'f', 0, 64) + "m"
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

// stripMergeTags reads Compose's !reset as empty and !override as the plain value, since Skiff reads one file with nothing to merge.
// expandMerges writes YAML merge keys (<<: *anchor) out as ordinary keys, so every later step sees what Compose sees. Keys written beside the merge win, and earlier sources in a list win over later ones.
func expandMerges(n *yaml.Node, seen map[*yaml.Node]bool) {
	if seen[n] {
		return
	}
	seen[n] = true
	if n.Alias != nil {
		expandMerges(n.Alias, seen)
	}
	for _, c := range n.Content {
		expandMerges(c, seen)
	}
	if n.Kind != yaml.MappingNode {
		return
	}
	var own, sources []*yaml.Node
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.ShortTag() != "!!merge" {
			own = append(own, k, v)
			continue
		}
		if v.Kind == yaml.SequenceNode {
			sources = append(sources, v.Content...)
		} else {
			sources = append(sources, v)
		}
	}
	if sources == nil {
		return
	}
	have := map[string]bool{}
	for i := 0; i < len(own); i += 2 {
		have[own[i].Value] = true
	}
	for _, src := range sources {
		for src.Kind == yaml.AliasNode && src.Alias != nil {
			src = src.Alias
		}
		if src.Kind != yaml.MappingNode {
			continue
		}
		for i := 0; i+1 < len(src.Content); i += 2 {
			if k := src.Content[i].Value; !have[k] {
				have[k] = true
				own = append(own, src.Content[i], src.Content[i+1])
			}
		}
	}
	n.Content = own
}

func stripMergeTags(n *yaml.Node, seen map[*yaml.Node]bool) {
	if seen[n] {
		return
	}
	seen[n] = true
	switch n.Tag {
	case "!reset":
		n.Kind, n.Tag, n.Value, n.Content = yaml.ScalarNode, "!!null", "", nil
	case "!override":
		n.Tag = ""
	}
	for _, c := range n.Content {
		stripMergeTags(c, seen)
	}
	if n.Alias != nil {
		stripMergeTags(n.Alias, seen)
	}
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
			out[n.Content[i].Value] = n.Content[i+1].Value
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
			m.Target = cleanTarget(t.Value)
		}
		m.Bind = kind == "bind"
		if r := mapValue(n, "read_only"); r != nil {
			m.ReadOnly = r.Value == "true"
		}
		return nil
	}
	parts := strings.Split(n.Value, ":")
	if len(parts) == 1 {
		m.Target = cleanTarget(parts[0])
		return nil
	}
	m.Source, m.Target = parts[0], cleanTarget(parts[1])
	m.Bind = strings.ContainsAny(m.Source, "/.~$")
	if len(parts) > 2 {
		m.ReadOnly = slices.Contains(strings.Split(parts[2], ","), "ro")
	}
	return nil
}

// rawUlimits reads both forms: nofile: 1024, and nofile: {soft: 1024, hard: 2048}.
type rawUlimits []string

func (u *rawUlimits) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		name, v := n.Content[i].Value, n.Content[i+1]
		limit := v.Value
		if v.Kind == yaml.MappingNode {
			soft, hard := mapValue(v, "soft"), mapValue(v, "hard")
			if soft == nil || hard == nil {
				continue
			}
			limit = soft.Value + ":" + hard.Value
		}
		*u = append(*u, name+"="+limit)
	}
	slices.Sort(*u)
	return nil
}

// rawHosts reads both forms: a list of name:address, and a map from name to address.
type rawHosts []string

func (h *rawHosts) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.SequenceNode:
		for _, c := range n.Content {
			*h = append(*h, strings.Replace(c.Value, "=", ":", 1))
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			*h = append(*h, n.Content[i].Value+":"+n.Content[i+1].Value)
		}
	}
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
