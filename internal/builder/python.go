package builder

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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

	for k, v := range in.env {
		env = withDefault(env, k, v)
	}
	return render(Plan{
		Base:       p.baseImage(in.tool),
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

	// tool is the package manager ("pip", "uv", "poetry" or "pipenv"); env is what it needs set, unless the app already sets it.
	tool string
	env  map[string]string

	// installsProject means the app's own console scripts end up on PATH.
	installsProject bool
}

// installer picks how dependencies get installed. requirements.txt stays first so a project that deploys today keeps installing the same way.
func (p *pythonBuilder) installer() pythonInstall {
	has := func(f string) bool { return fileExists(filepath.Join(p.dir, f)) }
	pp, _ := p.pyproject()
	switch {
	case has("requirements.txt"):
		return pythonInstall{tool: "pip", install: []string{"pip install --no-cache-dir -r requirements.txt"}}
	case has("uv.lock"):
		cache := []string{"pyproject.toml", "uv.lock"}
		if has(".python-version") {
			cache = append(cache, ".python-version")
		}
		return pythonInstall{
			tool:    "uv",
			cache:   cache,
			install: []string{"pip install --no-cache-dir uv && uv sync --locked --no-dev --no-install-project"},
			build:   []string{"uv sync --locked --no-dev"},
			// uv looks for the exact interpreter in .python-version (3.12.4), which the image's 3.12.x won't match; the image's own Python is the one to use.
			env:             map[string]string{"PATH": "/app/.venv/bin:$PATH", "UV_PYTHON": "/usr/local/bin/python"},
			installsProject: pp.uvInstallsProject(),
		}
	case has("poetry.lock"):
		// Poetry gets its own venv so an app that pins one of its dependencies (requests, say) can't break it; with virtualenvs disabled it installs the app into the image's Python.
		const poetry = "/opt/poetry/bin/poetry"
		return pythonInstall{
			tool:    "poetry",
			cache:   []string{"pyproject.toml", "poetry.lock"},
			install: []string{"python -m venv /opt/poetry && /opt/poetry/bin/pip install --no-cache-dir poetry && " + poetry + " install --no-root --only main --no-interaction"},
			build:   []string{poetry + " install --only main --no-interaction"},
			// Poetry would otherwise pick up a .venv copied from the developer's machine, which doesn't work in the image.
			env:             map[string]string{"POETRY_VIRTUALENVS_CREATE": "false", "POETRY_VIRTUALENVS_IN_PROJECT": "false"},
			installsProject: pp.poetryInstallsProject(),
		}
	case has("Pipfile.lock"):
		return pythonInstall{
			tool:    "pipenv",
			cache:   []string{"Pipfile", "Pipfile.lock"},
			install: []string{"pip install --no-cache-dir pipenv && pipenv install --system --deploy"},
		}
	case has("Pipfile"):
		return pythonInstall{
			tool:    "pipenv",
			cache:   []string{"Pipfile"},
			install: []string{"pip install --no-cache-dir pipenv && pipenv install --system --skip-lock"},
		}
	case has("pyproject.toml"):
		return pythonInstall{tool: "pip", install: []string{"pip install --no-cache-dir ."}, installsProject: true}
	}
	return pythonInstall{tool: "pip"}
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
	BuildSystem map[string]any `toml:"build-system"`
	Project     struct {
		Name           string         `toml:"name"`
		RequiresPython string         `toml:"requires-python"`
		Scripts        map[string]any `toml:"scripts"`
	} `toml:"project"`
	Tool struct {
		UV struct {
			Package *bool `toml:"package"`
		} `toml:"uv"`
		Poetry struct {
			Name         string         `toml:"name"`
			PackageMode  *bool          `toml:"package-mode"`
			Dependencies map[string]any `toml:"dependencies"`
			Scripts      map[string]any `toml:"scripts"`
		} `toml:"poetry"`
	} `toml:"tool"`
}

func (p *pythonBuilder) pyproject() (pyproject, bool) {
	var pp pyproject
	_, err := toml.DecodeFile(filepath.Join(p.dir, "pyproject.toml"), &pp)
	return pp, err == nil
}

// uvInstallsProject: uv only installs the project itself when it has a build system (or opts in), so a script in a project without one never exists.
func (pp pyproject) uvInstallsProject() bool {
	if pp.Tool.UV.Package != nil {
		return *pp.Tool.UV.Package
	}
	return pp.BuildSystem != nil
}

func (pp pyproject) poetryInstallsProject() bool {
	return pp.Tool.Poetry.PackageMode == nil || *pp.Tool.Poetry.PackageMode
}

// projectScript returns the console script to run: the only one, or else the one named after the project. Guessing between several would start the wrong thing.
func (p *pythonBuilder) projectScript() string {
	pp, ok := p.pyproject()
	if !ok {
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

func (p *pythonBuilder) baseImage(tool string) string {
	if v := pythonVersion(p.dir, tool); v != "" {
		return "python:" + v + "-alpine"
	}
	return "python:3-alpine"
}

// pythonVersion reads the Python the project asks for, as a major.minor image tag; "" means none declared. .python-version is an explicit choice and applies to every project. A requires-python floor is only a minimum, so it is used just for uv and Poetry, which are new here; Pipenv needs the Pipfile's version because --deploy refuses any other.
func pythonVersion(dir, tool string) string {
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
	switch tool {
	case "uv", "poetry":
		pp, ok := (&pythonBuilder{dir: dir}).pyproject()
		if !ok {
			return ""
		}
		spec := pp.Project.RequiresPython
		if s, isString := pp.Tool.Poetry.Dependencies["python"].(string); spec == "" && isString {
			spec = s
		}
		return lowestPython(spec)
	case "pipenv":
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
	}
	return ""
}

var (
	pythonRelease = regexp.MustCompile(`^(\d+)(?:\.(\d+))?`)
	pythonClause  = regexp.MustCompile(`(===|==|~=|!=|<=|>=|<|>|\^|~)?\s*(\d+(?:\.\d+)*)`)
)

// minAutoPython is the oldest minor picked from a specifier: Poetry can't be installed on python:3.8-alpine (its cffi dependency doesn't build there), and a floor is a minimum, not a preference.
const minAutoPython = 9

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

// lowestPython reads the lowest Python 3 a specifier allows, in either PEP 440 (">=3.9,<3.13", "~=3.10") or Poetry (">=3.10 <3.13", "^3.10", "~3.10", "3.11.*") syntax. Only the first of several "||" alternatives is read. Upper bounds name no version to pick. A range's floor is raised to minAutoPython; an exact pin ("==3.8", "~3.8") is left as written, since raising it would leave the specifier.
func lowestPython(spec string) string {
	spec, _, _ = strings.Cut(spec, "||")
	for _, m := range pythonClause.FindAllStringSubmatch(spec, -1) {
		op, ver := m[1], m[2]
		if op == "<" || op == "<=" || op == "!=" {
			continue
		}
		tag := pythonTag(ver)
		if tag == "" {
			continue
		}
		pinned := op == "" || op == "==" || op == "===" || op == "~" || (op == "~=" && strings.Count(ver, ".") >= 2)
		if minor, ok := strings.CutPrefix(tag, "3."); ok && !pinned {
			if n, _ := strconv.Atoi(minor); n < minAutoPython {
				tag = "3." + strconv.Itoa(minAutoPython)
			}
		}
		return tag
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
