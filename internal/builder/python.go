package builder

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

type pythonBuilder struct{ dir string }

func (p *pythonBuilder) Name() string { return "Python" }

func (p *pythonBuilder) detect() bool {
	for _, f := range []string{"requirements.txt", "pyproject.toml", "Pipfile", "main.py", "app.py", "server.py"} {
		if fileExists(filepath.Join(p.dir, f)) {
			return true
		}
	}
	return false
}

func (p *pythonBuilder) Dockerfile(port int, env map[string]string) (string, error) {
	in := p.installer()
	start, err := p.start(in.installsProject)
	if err != nil {
		return "", err
	}

	if in.venvOnPath {
		env = withDefault(env, "PATH", "/app/.venv/bin:$PATH")
	}
	return render(Plan{
		Base:       p.baseImage(),
		CacheFiles: in.cache,
		Install:    in.install,
		Build:      in.build,
		Env:        env,
		Start:      start,
		Port:       port,
	})
}

type pythonInstall struct {
	// cache files install before the source is copied, so the layer survives source-only edits; install runs after them, build after the source.
	cache   []string
	install []string
	build   []string

	// installsProject means the app's own console scripts end up on PATH.
	installsProject bool
	venvOnPath      bool
}

// installer picks how dependencies get installed. requirements.txt stays first so a project that deploys today keeps installing the same way.
func (p *pythonBuilder) installer() pythonInstall {
	has := func(f string) bool { return fileExists(filepath.Join(p.dir, f)) }
	switch {
	case has("requirements.txt"):
		return pythonInstall{install: []string{"pip install --no-cache-dir -r requirements.txt"}}
	case has("uv.lock"):
		cache := []string{"pyproject.toml", "uv.lock"}
		if has(".python-version") {
			cache = append(cache, ".python-version")
		}
		return pythonInstall{
			cache:           cache,
			install:         []string{"pip install --no-cache-dir uv && uv sync --locked --no-dev --no-install-project"},
			build:           []string{"uv sync --locked --no-dev"},
			installsProject: true,
			venvOnPath:      true,
		}
	case has("poetry.lock"):
		return pythonInstall{
			cache:           []string{"pyproject.toml", "poetry.lock"},
			install:         []string{"pip install --no-cache-dir poetry && poetry config virtualenvs.create false && poetry install --no-root --only main --no-interaction"},
			build:           []string{"poetry install --only main --no-interaction"},
			installsProject: true,
		}
	case has("Pipfile.lock"):
		return pythonInstall{
			cache:   []string{"Pipfile", "Pipfile.lock"},
			install: []string{"pip install --no-cache-dir pipenv && pipenv install --system --deploy"},
		}
	case has("Pipfile"):
		return pythonInstall{
			cache:   []string{"Pipfile"},
			install: []string{"pip install --no-cache-dir pipenv && pipenv install --system --skip-lock"},
		}
	case has("pyproject.toml"):
		return pythonInstall{install: []string{"pip install --no-cache-dir ."}, installsProject: true}
	}
	return pythonInstall{}
}

func (p *pythonBuilder) start(installed bool) ([]string, error) {
	if web := procfileWeb(filepath.Join(p.dir, "Procfile")); web != "" {
		return []string{"sh", "-c", web}, nil
	}
	for _, f := range []string{"main.py", "app.py", "server.py"} {
		if fileExists(filepath.Join(p.dir, f)) {
			return []string{"python", f}, nil
		}
	}
	if installed {
		if script := p.projectScript(); script != "" {
			return []string{script}, nil
		}
	}
	return nil, fmt.Errorf("couldn't find a Python entrypoint (main.py/app.py/server.py), a single [project.scripts] entry, or a Procfile web: command")
}

type pyproject struct {
	Project struct {
		Name           string         `toml:"name"`
		RequiresPython string         `toml:"requires-python"`
		Scripts        map[string]any `toml:"scripts"`
	} `toml:"project"`
	Tool struct {
		Poetry struct {
			Name    string         `toml:"name"`
			Scripts map[string]any `toml:"scripts"`
		} `toml:"poetry"`
	} `toml:"tool"`
}

// projectScript returns the console script to run: the only one, or else the one named after the project. Guessing between several would start the wrong thing.
func (p *pythonBuilder) projectScript() string {
	var pp pyproject
	if _, err := toml.DecodeFile(filepath.Join(p.dir, "pyproject.toml"), &pp); err != nil {
		return ""
	}
	scripts, name := pp.Project.Scripts, pp.Project.Name
	if len(scripts) == 0 {
		scripts, name = pp.Tool.Poetry.Scripts, pp.Tool.Poetry.Name
	}
	names := make([]string, 0, len(scripts))
	for s := range scripts {
		names = append(names, s)
	}
	sort.Strings(names)
	if len(names) == 1 {
		return names[0]
	}
	for _, s := range names {
		if normalizeDist(s) == normalizeDist(name) {
			return s
		}
	}
	return ""
}

// normalizeDist applies the PEP 503 name rules, so "My_App" and "my-app" compare equal.
func normalizeDist(s string) string {
	return strings.NewReplacer("_", "-", ".", "-").Replace(strings.ToLower(s))
}

func (p *pythonBuilder) baseImage() string {
	if v := pythonVersion(p.dir); v != "" {
		return "python:" + v + "-alpine"
	}
	return "python:3-alpine"
}

// pythonVersion reads the Python the project asks for (.python-version, requires-python, then Pipfile), as a major.minor image tag; "" means none declared.
func pythonVersion(dir string) string {
	if data, err := os.ReadFile(filepath.Join(dir, ".python-version")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
				if v := pythonTag(line); v != "" {
					return v
				}
				break
			}
		}
	}
	var pp pyproject
	if _, err := toml.DecodeFile(filepath.Join(dir, "pyproject.toml"), &pp); err == nil {
		if v := lowestPython(pp.Project.RequiresPython); v != "" {
			return v
		}
	}
	var pf struct {
		Requires struct {
			Version string `toml:"python_version"`
			Full    string `toml:"python_full_version"`
		} `toml:"requires"`
	}
	if _, err := toml.DecodeFile(filepath.Join(dir, "Pipfile"), &pf); err == nil {
		if v := pythonTag(pf.Requires.Version); v != "" {
			return v
		}
		return pythonTag(pf.Requires.Full)
	}
	return ""
}

var pythonRelease = regexp.MustCompile(`^(\d+)(?:\.(\d+))?`)

// pythonTag turns "3.12.4" into "3.12" and "3" into "3"; anything that isn't Python 3 gives "".
func pythonTag(v string) string {
	m := pythonRelease.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil || m[1] != "3" {
		return ""
	}
	if m[2] == "" {
		return "3"
	}
	return "3." + m[2]
}

// lowestPython reads the floor of a specifier such as ">=3.9,<3.13". Upper-bound-only specifiers name no version to pick.
func lowestPython(spec string) string {
	for _, clause := range strings.Split(spec, ",") {
		clause = strings.TrimSpace(clause)
		for _, op := range []string{"===", "==", "~=", ">="} {
			if rest, ok := strings.CutPrefix(clause, op); ok {
				return pythonTag(rest)
			}
		}
	}
	return ""
}

func withDefault(env map[string]string, key, value string) map[string]string {
	if _, ok := env[key]; ok {
		return env
	}
	out := make(map[string]string, len(env)+1)
	for k, v := range env {
		out[k] = v
	}
	out[key] = value
	return out
}

func procfileWeb(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "web:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "web:"))
		}
	}
	return ""
}
