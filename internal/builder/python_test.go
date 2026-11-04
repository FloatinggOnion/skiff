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

func TestPythonDockerfile(t *testing.T) {
	const scripted = "[project]\nname = \"myapp\"\n[project.scripts]\nmyapp = \"myapp:main\"\n"
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
			name:  "uv",
			files: map[string]string{"pyproject.toml": "[project]\nname = \"myapp\"\nrequires-python = \">=3.12\"\n[project.scripts]\nmyapp = \"myapp:main\"\n", "uv.lock": "x"},
			want: `FROM python:3.12-alpine
WORKDIR /app
ENV PATH="/app/.venv/bin:$PATH"
COPY pyproject.toml uv.lock ./
RUN pip install --no-cache-dir uv && uv sync --locked --no-dev --no-install-project
COPY . .
RUN uv sync --locked --no-dev
EXPOSE 8000
CMD ["myapp"]
`,
		},
		{
			name:  "uv copies .python-version into the cached layer",
			files: map[string]string{"pyproject.toml": scripted, "uv.lock": "x", ".python-version": "3.13\n"},
			want: `FROM python:3.13-alpine
WORKDIR /app
ENV PATH="/app/.venv/bin:$PATH"
COPY pyproject.toml uv.lock .python-version ./
RUN pip install --no-cache-dir uv && uv sync --locked --no-dev --no-install-project
COPY . .
RUN uv sync --locked --no-dev
EXPOSE 8000
CMD ["myapp"]
`,
		},
		{
			name:  "poetry",
			files: map[string]string{"pyproject.toml": scripted, "poetry.lock": "x"},
			want: `FROM python:3-alpine
WORKDIR /app
COPY pyproject.toml poetry.lock ./
RUN pip install --no-cache-dir poetry && poetry config virtualenvs.create false && poetry install --no-root --only main --no-interaction
COPY . .
RUN poetry install --only main --no-interaction
EXPOSE 8000
CMD ["myapp"]
`,
		},
		{
			name: "poetry with the older tool.poetry.scripts table",
			files: map[string]string{
				"pyproject.toml": "[tool.poetry]\nname = \"myapp\"\n[tool.poetry.scripts]\nserve = \"myapp:main\"\n",
				"poetry.lock":    "x",
			},
			want: `FROM python:3-alpine
WORKDIR /app
COPY pyproject.toml poetry.lock ./
RUN pip install --no-cache-dir poetry && poetry config virtualenvs.create false && poetry install --no-root --only main --no-interaction
COPY . .
RUN poetry install --only main --no-interaction
EXPOSE 8000
CMD ["serve"]
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
			map[string]string{"requirements.txt": "x", "pyproject.toml": "[project]\nname = \"myapp\"\n[project.scripts]\nmyapp = \"a:b\"\n"},
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
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"nothing declared", map[string]string{"main.py": "x"}, ""},
		{".python-version full", map[string]string{".python-version": "3.12.4\n"}, "3.12"},
		{".python-version minor", map[string]string{".python-version": "3.11"}, "3.11"},
		{".python-version skips comments", map[string]string{".python-version": "# pinned\n3.10.1\n"}, "3.10"},
		{".python-version for another interpreter", map[string]string{".python-version": "pypy3.10"}, ""},
		{"requires-python lower bound", map[string]string{"pyproject.toml": "[project]\nrequires-python = \">=3.11\"\n"}, "3.11"},
		{"requires-python range", map[string]string{"pyproject.toml": "[project]\nrequires-python = \">=3.9,<3.13\"\n"}, "3.9"},
		{"requires-python compatible release", map[string]string{"pyproject.toml": "[project]\nrequires-python = \"~=3.10\"\n"}, "3.10"},
		{"requires-python wildcard", map[string]string{"pyproject.toml": "[project]\nrequires-python = \"==3.12.*\"\n"}, "3.12"},
		{"requires-python upper bound only", map[string]string{"pyproject.toml": "[project]\nrequires-python = \"<4\"\n"}, ""},
		{"requires-python for Python 2", map[string]string{"pyproject.toml": "[project]\nrequires-python = \">=2.7\"\n"}, ""},
		{"Pipfile python_version", map[string]string{"Pipfile": "[requires]\npython_version = \"3.9\"\n"}, "3.9"},
		{"Pipfile python_full_version", map[string]string{"Pipfile": "[requires]\npython_full_version = \"3.8.2\"\n"}, "3.8"},
		{
			".python-version wins over pyproject",
			map[string]string{".python-version": "3.13", "pyproject.toml": "[project]\nrequires-python = \">=3.9\"\n"},
			"3.13",
		},
		{"unparseable pyproject is ignored", map[string]string{"pyproject.toml": "[project\n"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFiles(t, dir, tc.files)
			if got := pythonVersion(dir); got != tc.want {
				t.Errorf("pythonVersion() = %q, want %q", got, tc.want)
			}
		})
	}
}
