package builder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const (
	scripted = "[project]\nname = \"myapp\"\n[project.scripts]\nmyapp = \"myapp:main\"\n"
	// uv only installs a project that has a build system, which is what puts its scripts on PATH.
	packaged = buildSystem + scripted

	buildSystem = "[build-system]\nrequires = [\"setuptools\"]\nbuild-backend = \"setuptools.build_meta\"\n"

	poetryInstall = `RUN python -m venv /opt/poetry && /opt/poetry/bin/pip install --no-cache-dir poetry && /opt/poetry/bin/poetry install --no-root --only main --no-interaction
COPY . .
RUN /opt/poetry/bin/poetry install --only main --no-interaction
`
)

// scriptedFor is a project with a script and a requires-python, which has to sit in [project] ahead of the scripts table.
func scriptedFor(requiresPython string) string {
	return "[project]\nname = \"myapp\"\nrequires-python = \"" + requiresPython + "\"\n[project.scripts]\nmyapp = \"myapp:main\"\n"
}

func TestPythonDockerfile(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name:  "requirements.txt with app.py",
			files: map[string]string{"requirements.txt": "flask\n", "app.py": "x"},
			want: `FROM python:3-alpine
WORKDIR /app
COPY . .
RUN pip install --no-cache-dir -r requirements.txt
EXPOSE 8000
CMD ["python", "app.py"]
`,
		},
		{
			name:  "bare main.py",
			files: map[string]string{"main.py": "x"},
			want: `FROM python:3-alpine
WORKDIR /app
COPY . .
EXPOSE 8000
CMD ["python", "main.py"]
`,
		},
		{
			name:  "Procfile wins over main.py",
			files: map[string]string{"requirements.txt": "x", "main.py": "x", "Procfile": "web: gunicorn app:app\n"},
			want: `FROM python:3-alpine
WORKDIR /app
COPY . .
RUN pip install --no-cache-dir -r requirements.txt
EXPOSE 8000
CMD ["sh", "-c", "gunicorn app:app"]
`,
		},
		{
			name:  "pyproject only installs the project and runs its script",
			files: map[string]string{"pyproject.toml": scripted},
			want: `FROM python:3-alpine
WORKDIR /app
COPY . .
RUN pip install --no-cache-dir .
EXPOSE 8000
CMD ["myapp"]
`,
		},
		{
			name:  "main.py wins over a project script",
			files: map[string]string{"pyproject.toml": scripted, "main.py": "x"},
			want: `FROM python:3-alpine
WORKDIR /app
COPY . .
RUN pip install --no-cache-dir .
EXPOSE 8000
CMD ["python", "main.py"]
`,
		},
		{
			// a requires-python floor is a minimum, so it must not move a project that deploys today
			name:  "requirements.txt ignores a requires-python floor",
			files: map[string]string{"requirements.txt": "x", "main.py": "x", "pyproject.toml": "[project]\nrequires-python = \">=3.8\"\n"},
			want: `FROM python:3-alpine
WORKDIR /app
COPY . .
RUN pip install --no-cache-dir -r requirements.txt
EXPOSE 8000
CMD ["python", "main.py"]
`,
		},
		{
			name:  "pip install . ignores a requires-python floor",
			files: map[string]string{"pyproject.toml": scriptedFor(">=3.8")},
			want: `FROM python:3-alpine
WORKDIR /app
COPY . .
RUN pip install --no-cache-dir .
EXPOSE 8000
CMD ["myapp"]
`,
		},
		{
			name:  "requirements.txt follows .python-version",
			files: map[string]string{"requirements.txt": "x", "main.py": "x", ".python-version": "3.12.4\n"},
			want: `FROM python:3.12-alpine
WORKDIR /app
COPY . .
RUN pip install --no-cache-dir -r requirements.txt
EXPOSE 8000
CMD ["python", "main.py"]
`,
		},
		{
			name:  "requirements.txt ignores the Pipfile's version",
			files: map[string]string{"requirements.txt": "x", "main.py": "x", "Pipfile": "[requires]\npython_version = \"3.9\"\n"},
			want: `FROM python:3-alpine
WORKDIR /app
COPY . .
RUN pip install --no-cache-dir -r requirements.txt
EXPOSE 8000
CMD ["python", "main.py"]
`,
		},
		{
			name:  "uv",
			files: map[string]string{"pyproject.toml": buildSystem + scriptedFor(">=3.12"), "uv.lock": "x"},
			want: `FROM python:3.12-alpine
WORKDIR /app
ENV PATH="/app/.venv/bin:$PATH"
ENV UV_PYTHON="/usr/local/bin/python"
COPY pyproject.toml uv.lock ./
RUN pip install --no-cache-dir uv && uv sync --locked --no-dev --no-install-project
COPY . .
RUN uv sync --locked --no-dev
EXPOSE 8000
CMD ["myapp"]
`,
		},
		{
			// uv would look for 3.12.4 itself, and the image only has 3.12.x
			name:  "uv with an exact .python-version uses the image's Python",
			files: map[string]string{"pyproject.toml": packaged, "uv.lock": "x", ".python-version": "3.12.4\n"},
			want: `FROM python:3.12-alpine
WORKDIR /app
ENV PATH="/app/.venv/bin:$PATH"
ENV UV_PYTHON="/usr/local/bin/python"
COPY pyproject.toml uv.lock .python-version ./
RUN pip install --no-cache-dir uv && uv sync --locked --no-dev --no-install-project
COPY . .
RUN uv sync --locked --no-dev
EXPOSE 8000
CMD ["myapp"]
`,
		},
		{
			name:  "uv installs a project without a build system when it opts in",
			files: map[string]string{"pyproject.toml": scripted + "[tool.uv]\npackage = true\n", "uv.lock": "x"},
			want: `FROM python:3-alpine
WORKDIR /app
ENV PATH="/app/.venv/bin:$PATH"
ENV UV_PYTHON="/usr/local/bin/python"
COPY pyproject.toml uv.lock ./
RUN pip install --no-cache-dir uv && uv sync --locked --no-dev --no-install-project
COPY . .
RUN uv sync --locked --no-dev
EXPOSE 8000
CMD ["myapp"]
`,
		},
		{
			// Poetry lives in its own venv so an app that pins requests can't break it, and ignores a .venv copied from a laptop
			name:  "poetry",
			files: map[string]string{"pyproject.toml": scripted, "poetry.lock": "x"},
			want: `FROM python:3-alpine
WORKDIR /app
ENV POETRY_VIRTUALENVS_CREATE="false"
ENV POETRY_VIRTUALENVS_IN_PROJECT="false"
COPY pyproject.toml poetry.lock ./
` + poetryInstall + `EXPOSE 8000
CMD ["myapp"]
`,
		},
		{
			name: "poetry with the older tool.poetry layout reads its Python bound",
			files: map[string]string{
				"pyproject.toml": "[tool.poetry]\nname = \"myapp\"\n[tool.poetry.dependencies]\npython = \">=3.10,<3.13\"\n[tool.poetry.scripts]\nserve = \"myapp:main\"\n",
				"poetry.lock":    "x",
			},
			want: `FROM python:3.10-alpine
WORKDIR /app
ENV POETRY_VIRTUALENVS_CREATE="false"
ENV POETRY_VIRTUALENVS_IN_PROJECT="false"
COPY pyproject.toml poetry.lock ./
` + poetryInstall + `EXPOSE 8000
CMD ["serve"]
`,
		},
		{
			// 3.8 can't install Poetry (its cffi dependency doesn't build on that image), so the floor is raised
			name:  "poetry with a Python 3.8 floor builds on a Python Poetry can run on",
			files: map[string]string{"pyproject.toml": scriptedFor(">=3.8"), "poetry.lock": "x"},
			want: `FROM python:3.9-alpine
WORKDIR /app
ENV POETRY_VIRTUALENVS_CREATE="false"
ENV POETRY_VIRTUALENVS_IN_PROJECT="false"
COPY pyproject.toml poetry.lock ./
` + poetryInstall + `EXPOSE 8000
CMD ["myapp"]
`,
		},
		{
			name:  "pipenv with a lock",
			files: map[string]string{"Pipfile": "x", "Pipfile.lock": "x", "app.py": "x"},
			want: `FROM python:3-alpine
WORKDIR /app
COPY Pipfile Pipfile.lock ./
RUN pip install --no-cache-dir pipenv && pipenv install --system --deploy
COPY . .
EXPOSE 8000
CMD ["python", "app.py"]
`,
		},
		{
			name:  "pipenv without a lock",
			files: map[string]string{"Pipfile": "[requires]\npython_version = \"3.11\"\n", "app.py": "x"},
			want: `FROM python:3.11-alpine
WORKDIR /app
COPY Pipfile ./
RUN pip install --no-cache-dir pipenv && pipenv install --system --skip-lock
COPY . .
EXPOSE 8000
CMD ["python", "app.py"]
`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFiles(t, dir, tc.files)
			got, err := (&pythonBuilder{dir: dir}).Dockerfile(8000, nil)
			if err != nil {
				t.Fatalf("Dockerfile: %v", err)
			}
			if got != tc.want {
				t.Errorf("Dockerfile mismatch\n got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

func TestPythonKeepsTheAppsOwnEnv(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"pyproject.toml": packaged, "uv.lock": "x"})
	got, err := (&pythonBuilder{dir: dir}).Dockerfile(8000, map[string]string{"PATH": "/custom", "UV_PYTHON": "/usr/bin/python3"})
	if err != nil {
		t.Fatalf("Dockerfile: %v", err)
	}
	if !strings.Contains(got, `ENV PATH="/custom"`) || !strings.Contains(got, `ENV UV_PYTHON="/usr/bin/python3"`) {
		t.Errorf("the app's own env should win:\n%s", got)
	}
	if strings.Contains(got, "/app/.venv/bin") || strings.Contains(got, "/usr/local/bin/python") {
		t.Errorf("defaults leaked over the app's env:\n%s", got)
	}
}

func TestPythonInstallPriority(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"requirements.txt": "x", "uv.lock": "x", "poetry.lock": "x", "Pipfile": "x", "main.py": "x",
		"pyproject.toml": "[project]\nname = \"myapp\"\n",
	})
	got, err := (&pythonBuilder{dir: dir}).Dockerfile(8000, nil)
	if err != nil {
		t.Fatalf("Dockerfile: %v", err)
	}
	if !strings.Contains(got, "pip install --no-cache-dir -r requirements.txt") || strings.Contains(got, "uv ") {
		t.Errorf("requirements.txt should still win:\n%s", got)
	}
}

func TestPythonEntrypointErrors(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
	}{
		{"nothing to run", map[string]string{"requirements.txt": "x"}},
		{"pyproject without scripts", map[string]string{"pyproject.toml": "[project]\nname = \"myapp\"\n"}},
		{
			"several scripts, none named after the project",
			map[string]string{"pyproject.toml": "[project]\nname = \"myapp\"\n[project.scripts]\nserve = \"a:b\"\nmigrate = \"a:c\"\n"},
		},
		{
			// the script only exists on PATH if the project itself gets installed
			"script that requirements.txt never installs",
			map[string]string{"requirements.txt": "x", "pyproject.toml": scripted},
		},
		{
			// uv skips the project without a [build-system], so the script would build fine and then not exist at start
			"uv script in a project without a build system",
			map[string]string{"pyproject.toml": scripted, "uv.lock": "x"},
		},
		{
			"uv project that opts out of packaging",
			map[string]string{"pyproject.toml": packaged + "[tool.uv]\npackage = false\n", "uv.lock": "x"},
		},
		{
			"poetry project that opts out of packaging",
			map[string]string{"pyproject.toml": "[tool.poetry]\nname = \"myapp\"\npackage-mode = false\n[tool.poetry.scripts]\nmyapp = \"a:b\"\n", "poetry.lock": "x"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFiles(t, dir, tc.files)
			_, err := (&pythonBuilder{dir: dir}).Dockerfile(8000, nil)
			if err == nil || !strings.Contains(err.Error(), "Procfile") {
				t.Errorf("err = %v, want an entrypoint error that mentions the Procfile", err)
			}
		})
	}
}

func TestPythonScriptChoice(t *testing.T) {
	tests := []struct {
		name string
		toml string
		want string
	}{
		{"single script", "[project]\nname = \"x\"\n[project.scripts]\nrun = \"a:b\"\n", "run"},
		{"named after the project", "[project]\nname = \"My_App\"\n[project.scripts]\nmy-app = \"a:b\"\nmigrate = \"a:c\"\n", "my-app"},
		{"ambiguous", "[project]\nname = \"x\"\n[project.scripts]\na = \"a:b\"\nb = \"a:c\"\n", ""},
		{"none", "[project]\nname = \"x\"\n", ""},
		{"poetry file-style script", "[tool.poetry]\nname = \"x\"\n[tool.poetry.scripts]\nrun = { reference = \"run.py\", type = \"file\" }\n", "run"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFiles(t, dir, map[string]string{"pyproject.toml": tc.toml})
			if got := (&pythonBuilder{dir: dir}).projectScript(); got != tc.want {
				t.Errorf("projectScript() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPythonVersion(t *testing.T) {
	requires := func(spec string) map[string]string {
		return map[string]string{"pyproject.toml": "[project]\nrequires-python = \"" + spec + "\"\n"}
	}
	poetryPython := func(spec string) map[string]string {
		return map[string]string{"pyproject.toml": "[tool.poetry.dependencies]\npython = \"" + spec + "\"\n"}
	}
	tests := []struct {
		name  string
		tool  string
		files map[string]string
		want  string
	}{
		{"nothing declared", "pip", map[string]string{"main.py": "x"}, ""},

		// .python-version is an explicit choice, so every tool follows it
		{".python-version full", "pip", map[string]string{".python-version": "3.12.4\n"}, "3.12"},
		{".python-version minor", "pip", map[string]string{".python-version": "3.11"}, "3.11"},
		{".python-version skips comments", "pip", map[string]string{".python-version": "# pinned\n3.10.1\n"}, "3.10"},
		{".python-version for another interpreter", "pip", map[string]string{".python-version": "pypy3.10"}, ""},
		{".python-version with uv", "uv", map[string]string{".python-version": "3.12.4"}, "3.12"},
		{".python-version with poetry", "poetry", map[string]string{".python-version": "3.11"}, "3.11"},
		{".python-version with pipenv", "pipenv", map[string]string{".python-version": "3.10"}, "3.10"},
		{".python-version is not raised to the minimum", "uv", map[string]string{".python-version": "3.8"}, "3.8"},
		{
			".python-version wins over requires-python",
			"uv", map[string]string{".python-version": "3.13", "pyproject.toml": "[project]\nrequires-python = \">=3.9\"\n"},
			"3.13",
		},

		// a floor is only a minimum, so it is ignored by tools that already deploy without it
		{"requires-python is ignored by pip", "pip", requires(">=3.11"), ""},
		{"the Pipfile is ignored by pip", "pip", map[string]string{"Pipfile": "[requires]\npython_version = \"3.9\"\n"}, ""},
		{"requires-python is ignored by pipenv", "pipenv", requires(">=3.11"), ""},

		{"uv lower bound", "uv", requires(">=3.11"), "3.11"},
		{"uv range", "uv", requires(">=3.10,<3.13"), "3.10"},
		{"uv range, upper bound first", "uv", requires("<3.13,>=3.10"), "3.10"},
		{"uv range with spaces", "uv", requires(">= 3.10, < 3.13"), "3.10"},
		{"uv compatible release", "uv", requires("~=3.10"), "3.10"},
		{"uv wildcard", "uv", requires("==3.12.*"), "3.12"},
		{"uv upper bound only", "uv", requires("<4"), ""},
		{"uv for Python 2", "uv", requires(">=2.7"), ""},
		{"uv old floor is raised", "uv", requires(">=3.8"), "3.9"},
		{"uv exact pin is not raised", "uv", requires("==3.8.*"), "3.8"},
		{"uv first alternative", "uv", requires(">=3.10 || >=3.8"), "3.10"},
		{"uv unparseable pyproject", "uv", map[string]string{"pyproject.toml": "[project\n"}, ""},

		// Poetry: PEP 621 and the older [tool.poetry.dependencies] python field, with its caret and tilde syntax
		{"poetry PEP 621", "poetry", requires(">=3.11"), "3.11"},
		{"poetry old layout, range", "poetry", poetryPython(">=3.10,<3.13"), "3.10"},
		{"poetry old layout, space-separated range", "poetry", poetryPython(">=3.10 <3.13"), "3.10"},
		{"poetry caret", "poetry", poetryPython("^3.11"), "3.11"},
		{"poetry caret on an old floor is raised", "poetry", poetryPython("^3.8"), "3.9"},
		{"poetry tilde", "poetry", poetryPython("~3.10"), "3.10"},
		{"poetry tilde with a patch", "poetry", poetryPython("~3.10.2"), "3.10"},
		{"poetry tilde pin is not raised", "poetry", poetryPython("~3.8"), "3.8"},
		{"poetry wildcard", "poetry", poetryPython("3.11.*"), "3.11"},
		{"poetry bare version", "poetry", poetryPython("3.11"), "3.11"},
		{"poetry union reads the first alternative", "poetry", poetryPython("^3.10 || ^3.12"), "3.10"},
		{"poetry any version", "poetry", poetryPython("*"), ""},
		{"poetry old floor", "poetry", poetryPython(">=3.8"), "3.9"},
		{
			"poetry prefers requires-python",
			"poetry",
			map[string]string{"pyproject.toml": "[project]\nrequires-python = \">=3.12\"\n[tool.poetry.dependencies]\npython = \"^3.10\"\n"},
			"3.12",
		},
		{
			"poetry python written as a table",
			"poetry",
			map[string]string{"pyproject.toml": "[tool.poetry.dependencies]\npython = { version = \"^3.10\" }\n"},
			"",
		},

		// the Pipfile's version is exact, and pipenv --deploy refuses any other
		{"pipenv python_version", "pipenv", map[string]string{"Pipfile": "[requires]\npython_version = \"3.9\"\n"}, "3.9"},
		{"pipenv python_full_version", "pipenv", map[string]string{"Pipfile": "[requires]\npython_full_version = \"3.8.2\"\n"}, "3.8"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFiles(t, dir, tc.files)
			if got := pythonVersion(dir, tc.tool); got != tc.want {
				t.Errorf("pythonVersion(%q) = %q, want %q", tc.tool, got, tc.want)
			}
		})
	}
}
