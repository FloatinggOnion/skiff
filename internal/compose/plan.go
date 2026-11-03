package compose

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

type Kind string

const (
	KindApp      Kind = "app"
	KindWorker   Kind = "worker"
	KindDatabase Kind = "database"
)

// Item is one thing Skiff would create for a Compose service.
type Item struct {
	Service  string
	Name     string
	Kind     Kind
	Image    string
	Build    *Build
	Command  []string
	Env      map[string]string
	Port     int
	Public   bool
	Volumes  []PlanVolume
	Replicas int
	Memory   string
	CPUs     string
	After    []string
	// HealthPath is the HTTP path from the service's healthcheck, when it has one.
	HealthPath string
	// WorkerOf names the service whose image and settings a worker shares.
	WorkerOf string
	// Database fields.
	Engine                 string
	DBUser, DBPassword, DB string
}

type PlanVolume struct{ Name, Path string }

type Plan struct {
	Stack string
	Items []Item
	// Problems must be fixed before this file can be used.
	Problems []string
	// Notes are things that will work differently, or be ignored.
	Notes []string
	// Unset lists variables the file uses that have no value yet.
	Unset []string
}

var managedEngines = map[string]string{"postgres": "postgres", "mysql": "mysql", "mongo": "mongodb", "redis": "redis"}

var unsupportedKeys = map[string]string{
	"privileged":   "needs privileges Skiff doesn't grant",
	"network_mode": "Skiff manages the network",
	"devices":      "Skiff doesn't pass devices through",
	"extends":      "isn't supported yet, so merge it into the file",
	"secrets":      "isn't supported yet, so use environment variables",
	"configs":      "isn't supported yet, so build the file into the image",
}

var nameUnsafe = regexp.MustCompile(`[^a-z0-9-]+`)

func safeName(s string) string {
	return strings.Trim(nameUnsafe.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

// ParseEnvFile reads KEY=value lines.
func ParseEnvFile(data []byte) map[string]string {
	out := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(strings.TrimPrefix(line, "export "), "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		out[strings.TrimSpace(k)] = v
	}
	return out
}

// BuildPlan works out what a parsed file becomes. readFile loads env_file entries, relative to the Compose file.
func BuildPlan(stack string, f *File, readFile func(string) ([]byte, error)) *Plan {
	p := &Plan{Stack: safeName(stack)}
	if p.Stack == "" {
		p.Problems = append(p.Problems, "the stack needs a name made of letters, digits or dashes")
	}
	p.Unset = dedupe(f.Unset)
	for _, v := range p.Unset {
		p.Problems = append(p.Problems, fmt.Sprintf("%s needs a value. For one that can stay empty, write ${%s:-} in the file", v, v))
	}

	resolved := map[string]Service{}
	for _, name := range f.Order {
		s := f.Services[name]
		env := map[string]string{}
		for _, file := range s.EnvFiles {
			data, err := readFile(file)
			if err != nil {
				p.Problems = append(p.Problems, fmt.Sprintf("%s: can't read env_file %s", name, file))
				continue
			}
			for k, v := range ParseEnvFile(data) {
				env[k] = v
			}
		}
		for k, v := range s.Environment {
			env[k] = v
		}
		s.Environment = env
		resolved[name] = s
	}

	// A named volume can only be shared between an app and its workers.
	volumeUsers := map[string][]string{}
	for _, name := range f.Order {
		for _, m := range resolved[name].Volumes {
			if m.Source != "" && !m.Bind {
				volumeUsers[m.Source] = append(volumeUsers[m.Source], name)
			}
		}
	}

	byName := map[string]*Item{}
	for _, name := range f.Order {
		s := resolved[name]
		it := Item{
			Service: name, Name: p.Stack + "-" + safeName(name), Kind: KindApp, Image: s.Image, Build: s.Build,
			Command: s.Command, Env: s.Environment, Replicas: s.Replicas, Memory: s.Memory, CPUs: s.CPUs, After: s.DependsOn,
		}
		if safeName(name) == "" {
			p.Problems = append(p.Problems, fmt.Sprintf("service %q needs a name made of letters, digits or dashes", name))
		}
		if s.Image == "" && s.Build == nil {
			p.Problems = append(p.Problems, name+": needs an image or a build")
		}
		for _, key := range f.Extra[name] {
			if why, bad := unsupportedKeys[key]; bad {
				p.Problems = append(p.Problems, fmt.Sprintf("%s: %s %s", name, key, why))
			} else {
				p.Notes = append(p.Notes, fmt.Sprintf("%s: %s is ignored", name, key))
			}
		}
		if s.Build != nil && s.Image != "" {
			p.Notes = append(p.Notes, name+": builds from source, so image is ignored")
			it.Image = ""
		}
		for _, d := range s.DependsOn {
			if _, ok := f.Services[d]; !ok {
				p.Problems = append(p.Problems, fmt.Sprintf("%s: depends on %s, which isn't in this file", name, d))
			}
		}

		if engine := managedEngine(s); engine != "" {
			applyDatabase(&it, engine, s, p)
			byName[name] = &it
			continue
		}

		switch {
		case len(s.Ports) > 0:
			it.Port, it.Public = s.Ports[0].Target, true
			if len(s.Ports) > 1 {
				p.Notes = append(p.Notes, fmt.Sprintf("%s: only port %d is served, because Skiff serves one port per app", name, it.Port))
			}
		case len(s.Expose) > 0:
			it.Port = s.Expose[0]
		}
		for _, m := range s.Volumes {
			switch {
			case m.Bind:
				p.Problems = append(p.Problems, fmt.Sprintf("%s: %s is a path on this machine, which won't exist on the server. Build it into the image or use a named volume", name, m.Source))
			default:
				vol := safeName(m.Source)
				if vol == "" {
					vol = safeName(m.Target)
				}
				it.Volumes = append(it.Volumes, PlanVolume{Name: vol, Path: m.Target})
			}
		}
		if len(it.Volumes) > 5 {
			p.Problems = append(p.Problems, name+": Skiff allows 5 volumes per app")
		}
		it.HealthPath = healthPath(s.Healthcheck)
		if len(s.Healthcheck) > 0 && it.HealthPath == "" {
			p.Notes = append(p.Notes, name+": its healthcheck isn't an HTTP request, so Skiff checks that the port answers instead")
		}
		byName[name] = &it
	}

	// A service built like another one, differing only by command, is that service's worker.
	for _, name := range f.Order {
		it := byName[name]
		if it.Kind != KindApp || len(it.Command) == 0 || it.Port != 0 {
			continue
		}
		for _, other := range f.Order {
			o := byName[other]
			if other == name || o.Kind != KindApp || o.WorkerOf != "" || !sameBuild(it, o) || o.Port == 0 {
				continue
			}
			if !sameEnv(it.Env, o.Env) || !slices.Equal(it.Volumes, o.Volumes) {
				break
			}
			it.Kind, it.WorkerOf = KindWorker, other
			break
		}
	}

	for vol, users := range volumeUsers {
		users = dedupe(users)
		if len(users) < 2 {
			continue
		}
		owner := ""
		ok := true
		for _, u := range users {
			it := byName[u]
			if it == nil || it.Kind == KindDatabase {
				ok = false
				break
			}
			root := u
			if it.WorkerOf != "" {
				root = it.WorkerOf
			}
			if owner != "" && owner != root {
				ok = false
			}
			owner = root
		}
		if !ok {
			p.Problems = append(p.Problems, fmt.Sprintf("volume %s is shared by %s, but only an app and its workers can share one", vol, strings.Join(users, ", ")))
		}
	}

	order, cycle := startOrder(f.Order, resolved)
	if cycle != "" {
		p.Problems = append(p.Problems, "depends_on goes in a circle through "+cycle)
	}
	for _, name := range order {
		p.Items = append(p.Items, *byName[name])
	}
	if !slices.ContainsFunc(p.Items, func(i Item) bool { return i.Public }) {
		p.Notes = append(p.Notes, "no service publishes a port, so nothing will be reachable from outside")
	}
	slices.Sort(p.Problems)
	slices.Sort(p.Notes)
	return p
}

func managedEngine(s Service) string {
	if s.Build != nil || s.Image == "" {
		return ""
	}
	repo, _, _ := strings.Cut(s.Image, "@")
	if i := strings.LastIndex(repo, ":"); i > strings.LastIndex(repo, "/") {
		repo = repo[:i]
	}
	repo = strings.TrimPrefix(strings.TrimPrefix(repo, "docker.io/"), "library/")
	return managedEngines[repo]
}

func applyDatabase(it *Item, engine string, s Service, p *Plan) {
	it.Kind, it.Engine = KindDatabase, engine
	env := s.Environment
	switch engine {
	case "postgres":
		it.DBUser, it.DBPassword, it.DB = env["POSTGRES_USER"], env["POSTGRES_PASSWORD"], env["POSTGRES_DB"]
		if it.DBUser == "" {
			it.DBUser = "postgres"
		}
		if it.DB == "" {
			it.DB = it.DBUser
		}
	case "mysql":
		it.DBUser, it.DBPassword, it.DB = env["MYSQL_USER"], env["MYSQL_PASSWORD"], env["MYSQL_DATABASE"]
		if it.DBUser == "" {
			it.DBUser, it.DBPassword = "root", env["MYSQL_ROOT_PASSWORD"]
		}
	case "mongodb":
		it.DBUser, it.DBPassword, it.DB = env["MONGO_INITDB_ROOT_USERNAME"], env["MONGO_INITDB_ROOT_PASSWORD"], env["MONGO_INITDB_DATABASE"]
	case "redis":
		for i, a := range s.Command {
			if a == "--requirepass" && i+1 < len(s.Command) {
				it.DBPassword = s.Command[i+1]
			}
		}
	}
	if it.DBPassword == "" && engine == "redis" {
		p.Notes = append(p.Notes, it.Service+": has no password, as in your file. Only this stack's services can reach it")
	} else if it.DBPassword == "" && len(p.Unset) == 0 {
		p.Notes = append(p.Notes, it.Service+": has no password, so Skiff generates one. Apps that connect with a hardcoded URL need updating")
	}
	if len(s.Ports) > 0 {
		p.Notes = append(p.Notes, it.Service+": stays private, so its published port is ignored. Turn on public access in the dashboard if you need it")
	}
	for _, m := range s.Volumes {
		if m.Bind {
			p.Notes = append(p.Notes, fmt.Sprintf("%s: starts empty, so the data in %s isn't brought over. Load it with a restore", it.Service, m.Source))
		}
	}
}

var healthURL = regexp.MustCompile(`https?://[^/\s'"]+(/[^\s'"]*)?`)

func healthPath(test []string) string {
	m := healthURL.FindStringSubmatch(strings.Join(test, " "))
	if m == nil {
		return ""
	}
	if m[1] == "" {
		return "/"
	}
	return m[1]
}

func sameBuild(a, b *Item) bool {
	if a.Build != nil && b.Build != nil {
		return *a.Build == *b.Build
	}
	return a.Build == nil && b.Build == nil && a.Image == b.Image
}

func sameEnv(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

// startOrder lists services after the ones they depend on, keeping file order otherwise.
func startOrder(names []string, services map[string]Service) ([]string, string) {
	state := map[string]int{}
	var out []string
	cycle := ""
	var visit func(string)
	visit = func(n string) {
		switch state[n] {
		case 1:
			if cycle == "" {
				cycle = n
			}
			return
		case 2:
			return
		}
		state[n] = 1
		for _, d := range services[n].DependsOn {
			if _, ok := services[d]; ok {
				visit(d)
			}
		}
		state[n] = 2
		out = append(out, n)
	}
	for _, n := range names {
		visit(n)
	}
	return out, cycle
}

func dedupe(in []string) []string {
	var out []string
	for _, s := range in {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}
