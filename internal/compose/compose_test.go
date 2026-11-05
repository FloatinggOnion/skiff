package compose

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

const backend = `
x-common: &env
  LOG_LEVEL: info
services:
  api:
    build:
      context: ./api
      dockerfile: Dockerfile.prod
    ports: ["8080:3000"]
    environment:
      DATABASE_URL: postgres://app:${DB_PASSWORD:-devpass}@db:5432/shop
      REDIS_URL: redis://cache:6379
    depends_on:
      db: {condition: service_healthy}
      cache: {condition: service_started}
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:3000/healthz"]
    deploy:
      replicas: 2
      resources: {limits: {memory: 512m, cpus: "0.5"}}
    restart: always
  worker:
    build:
      context: ./api
      dockerfile: Dockerfile.prod
    command: node worker.js --queue "high priority"
    environment:
      DATABASE_URL: postgres://app:${DB_PASSWORD:-devpass}@db:5432/shop
      REDIS_URL: redis://cache:6379
    depends_on: [db]
  db:
    image: postgres:15
    environment:
      POSTGRES_USER: app
      POSTGRES_PASSWORD: ${DB_PASSWORD:-devpass}
      POSTGRES_DB: shop
    volumes: [dbdata:/var/lib/postgresql/data]
  cache:
    image: redis:7-alpine
    command: redis-server --appendonly yes
  search:
    image: getmeili/meilisearch:v1.6
    expose: ["7700"]
    volumes: [search:/meili_data]
volumes: {dbdata: {}, search: {}}
`

func noEnv(string) (string, bool) { return "", false }

func noFiles(name string) ([]byte, error) { return nil, errors.New("missing " + name) }

func plan(t *testing.T, src string) *Plan {
	t.Helper()
	f, err := Parse([]byte(src), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	return BuildPlan("shop", f, noFiles, nil)
}

func item(t *testing.T, p *Plan, service string) Item {
	t.Helper()
	for _, i := range p.Items {
		if i.Service == service {
			return i
		}
	}
	t.Fatalf("no item for %s in %+v", service, p.Items)
	return Item{}
}

func TestPlanForATypicalBackend(t *testing.T) {
	p := plan(t, backend)
	if len(p.Problems) != 0 {
		t.Fatalf("problems: %v", p.Problems)
	}
	api := item(t, p, "api")
	if api.Name != "shop-api" || api.Kind != KindApp || !api.Public || api.Port != 3000 || api.Replicas != 2 ||
		api.Memory != "512m" || api.CPUs != "0.5" || api.HealthPath != "/healthz" || *api.Build != (Build{"./api", "Dockerfile.prod"}) {
		t.Errorf("api: %+v", api)
	}
	if got := api.Env["DATABASE_URL"]; got != "postgres://app:devpass@db:5432/shop" {
		t.Errorf("DATABASE_URL = %q", got)
	}
	w := item(t, p, "worker")
	if w.Kind != KindWorker || w.WorkerOf != "api" || strings.Join(w.Command, "|") != "node|worker.js|--queue|high priority" {
		t.Errorf("worker: %+v", w)
	}
	db := item(t, p, "db")
	if db.Kind != KindDatabase || db.Engine != "postgres" || db.DBUser != "app" || db.DBPassword != "devpass" || db.DB != "shop" || db.Image != "postgres:15" {
		t.Errorf("db: %+v", db)
	}
	if c := item(t, p, "cache"); c.Kind != KindDatabase || c.Engine != "redis" {
		t.Errorf("cache: %+v", c)
	}
	s := item(t, p, "search")
	if s.Kind != KindApp || s.Public || s.Port != 7700 || len(s.Volumes) != 1 || s.Volumes[0] != (PlanVolume{"search", "/meili_data"}) {
		t.Errorf("search: %+v", s)
	}
	pos := map[string]int{}
	for i, it := range p.Items {
		pos[it.Service] = i
	}
	if pos["db"] > pos["api"] || pos["cache"] > pos["api"] || pos["db"] > pos["worker"] {
		t.Errorf("start order: %+v", pos)
	}
	if !strings.Contains(strings.Join(p.Notes, "\n"), "api: restart is ignored") {
		t.Errorf("notes: %v", p.Notes)
	}
}

func TestInterpolation(t *testing.T) {
	env := map[string]string{"SET": "value", "EMPTY": ""}
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	f, err := Parse([]byte(`services:
  a:
    image: nginx
    environment:
      PLAIN: $SET
      BRACED: ${SET}
      DEFAULT_UNSET: ${NOPE:-fallback}
      DEFAULT_EMPTY: ${EMPTY:-fallback}
      DASH_EMPTY: ${EMPTY-fallback}
      PLUS: ${SET:+yes}
      PLUS_UNSET: ${NOPE:+yes}
      ESCAPED: $$SET
      MISSING: ${GONE}
`), lookup)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"PLAIN": "value", "BRACED": "value", "DEFAULT_UNSET": "fallback", "DEFAULT_EMPTY": "fallback",
		"DASH_EMPTY": "", "PLUS": "yes", "PLUS_UNSET": "", "ESCAPED": "$SET", "MISSING": ""}
	got := f.Services["a"].Environment
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if len(f.Unset) != 1 || f.Unset[0] != "GONE" {
		t.Errorf("unset: %v", f.Unset)
	}
	if _, err := Parse([]byte("services:\n  a:\n    image: ${NEEDED:?set this}\n"), lookup); err == nil || !strings.Contains(err.Error(), "set this") {
		t.Errorf("required variable: %v", err)
	}
}

func TestFormsOfEachKey(t *testing.T) {
	f, err := Parse([]byte(`services:
  a:
    build: ./app
    command: ["serve", "--port", "1"]
    environment: ["A=1", "B=two=2", "C"]
    env_file: [{path: one.env}, two.env]
    ports:
      - "127.0.0.1:8080:80/tcp"
      - target: 443
        published: "8443"
      - "9000"
    volumes:
      - /anon
      - named:/data
      - ./local:/src:ro
      - {type: bind, source: /etc/x, target: /x}
      - {type: volume, source: v2, target: /y}
`), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	s := f.Services["a"]
	if s.Build.Context != "./app" || s.Environment["B"] != "two=2" || len(s.EnvFiles) != 2 || s.EnvFiles[0] != "one.env" {
		t.Errorf("%+v", s)
	}
	if s.Ports[0] != (Port{"8080", 80}) || s.Ports[1] != (Port{"8443", 443}) || s.Ports[2] != (Port{"", 9000}) {
		t.Errorf("ports: %+v", s.Ports)
	}
	v := s.Volumes
	if v[0] != (Mount{Target: "/anon"}) || v[1] != (Mount{Source: "named", Target: "/data", Bind: false}) || v[2] != (Mount{Source: "./local", Target: "/src", Bind: true, ReadOnly: true}) ||
		v[3] != (Mount{Source: "/etc/x", Target: "/x", Bind: true}) || v[4] != (Mount{Source: "v2", Target: "/y", Bind: false}) {
		t.Errorf("volumes: %+v", v)
	}
}

func TestProblemsAreReported(t *testing.T) {
	for name, tc := range map[string]struct{ src, want string }{
		"system folder":   {"services:\n  a:\n    image: x\n    volumes: [/etc/app:/app]\n", "system folder"},
		"no image":        {"services:\n  a:\n    command: hi\n", "needs an image or a build"},
		"privileged":      {"services:\n  a:\n    image: x\n    privileged: true\n", "privileged"},
		"host network":    {"services:\n  a:\n    image: x\n    network_mode: host\n", "network_mode"},
		"unknown depends": {"services:\n  a:\n    image: x\n    depends_on: [ghost]\n", "ghost"},
		"cycle":           {"services:\n  a:\n    image: x\n    depends_on: [b]\n  b:\n    image: x\n    depends_on: [a]\n", "circle"},
		"shared volume":   {"services:\n  a:\n    image: x\n    volumes: [v:/a]\n  b:\n    image: y\n    volumes: [v:/b]\n", "volume v is shared"},
		"missing envfile": {"services:\n  a:\n    image: x\n    env_file: gone.env\n", "gone.env"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := strings.Join(plan(t, tc.src).Problems, "\n"); !strings.Contains(got, tc.want) {
				t.Errorf("problems %q, want %q", got, tc.want)
			}
		})
	}
	if _, err := Parse([]byte("nope: 1\n"), noEnv); err == nil {
		t.Error("a file with no services should fail")
	}
	if _, err := Parse([]byte(":\n\t- bad"), noEnv); err == nil {
		t.Error("invalid YAML should fail")
	}
}

func TestWorkerNeedsMatchingEnv(t *testing.T) {
	p := plan(t, `services:
  web:
    image: app:1
    ports: ["80:80"]
    environment: {A: "1"}
  jobs:
    image: app:1
    command: run-jobs
    environment: {A: "2"}
`)
	if j := item(t, p, "jobs"); j.Kind != KindApp {
		t.Errorf("different settings should stay a separate app: %+v", j)
	}
}

func TestStockAndCustomDatabases(t *testing.T) {
	for image, want := range map[string]string{
		"postgres": "postgres", "postgres:16-alpine": "postgres", "docker.io/library/postgres:15": "postgres", "mysql:8": "mysql",
		"mongo:7": "mongodb", "redis:7": "redis", "pgvector/pgvector:pg16": "", "bitnami/redis": "", "mariadb:11": "", "registry.example.com/postgres:15": "",
	} {
		if got := managedEngine(Service{Image: image}); got != want {
			t.Errorf("%s = %q, want %q", image, got, want)
		}
	}
}

func TestEnvFile(t *testing.T) {
	got := ParseEnvFile([]byte("# note\nA=1\nexport B=2\nC=\"quoted value\"\nD='single'\n\nbroken\nE=a=b\n"))
	want := map[string]string{"A": "1", "B": "2", "C": "quoted value", "D": "single", "E": "a=b"}
	if len(got) != len(want) {
		t.Errorf("%v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q", k, got[k])
		}
	}
	f, _ := Parse([]byte("services:\n  a:\n    image: x\n    env_file: .env\n    environment: {A: inline}\n"), noEnv)
	p := BuildPlan("s", f, func(string) ([]byte, error) { return []byte("A=file\nB=file\n"), nil }, nil)
	if a := item(t, p, "a"); a.Env["A"] != "inline" || a.Env["B"] != "file" {
		t.Errorf("environment should win over env_file: %v", a.Env)
	}
}

func TestMissingVariablesBlockTheStack(t *testing.T) {
	src := "services:\n  a:\n    image: x\n    environment: {P: \"${SECRET}\", O: \"${OPTIONAL:-}\"}\n"
	p := plan(t, src)
	if len(p.Unset) != 1 || p.Unset[0] != "SECRET" {
		t.Fatalf("unset: %v", p.Unset)
	}
	if !strings.Contains(strings.Join(p.Problems, "\n"), "SECRET needs a value") {
		t.Fatalf("problems: %v", p.Problems)
	}
	f, err := Parse([]byte(src), func(k string) (string, bool) { return "v", k == "SECRET" })
	if err != nil {
		t.Fatal(err)
	}
	if got := BuildPlan("s", f, noFiles, nil); len(got.Problems) != 0 {
		t.Fatalf("with the value given: %v", got.Problems)
	}
}

func TestMissingPasswordOnlyAsksForIt(t *testing.T) {
	const file = "services:\n  db:\n    image: postgres:16-alpine\n    environment:\n      POSTGRES_PASSWORD: ${DB_PASSWORD}\n"
	for _, n := range plan(t, file).Notes {
		if strings.Contains(n, "generates one") {
			t.Fatalf("told to enter a password and that one will be generated: %q", n)
		}
	}
}

func TestMounts(t *testing.T) {
	repo := func(p string) PathKind {
		switch p {
		case "./nginx.conf":
			return PathFile
		case "./site", "./seed":
			return PathDir
		}
		return PathMissing
	}
	const file = `services:
  a:
    image: x
    ports: ["80"]
    volumes:
      - ./nginx.conf:/etc/nginx/nginx.conf:ro
      - ./site:/www:ro
      - ./seed:/seed
      - ./data:/data
      - /opt/stuff:/stuff
      - /etc/localtime:/etc/localtime:ro
`
	f, err := Parse([]byte(file), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	p := BuildPlan("s", f, noFiles, repo)
	if len(p.Problems) != 0 {
		t.Fatalf("problems: %v", p.Problems)
	}
	a := item(t, p, "a")
	wantFiles := []PlanFile{{"./nginx.conf", "/etc/nginx/nginx.conf"}, {"./site", "/www"}, {"./seed", "/seed"}}
	if !slices.Equal(a.Files, wantFiles) {
		t.Errorf("files: %v", a.Files)
	}
	wantVolumes := []PlanVolume{{"seed", "/seed"}, {"data", "/data"}, {"opt-stuff", "/stuff"}}
	if !slices.Equal(a.Volumes, wantVolumes) {
		t.Errorf("volumes: %v", a.Volumes)
	}

	for _, tc := range []struct{ name, mount, want string }{
		{"docker socket", "/var/run/docker.sock:/var/run/docker.sock", "Docker socket"},
		{"system folder", "/lib/modules:/lib/modules", "system folder"},
		{"nested system folder", "/etc/pihole/:/etc/pihole", "system folder"},
		{"whole disk", "/:/host", "system folder"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, err := Parse([]byte("services:\n  a:\n    image: x\n    volumes: [\""+tc.mount+"\"]\n"), noEnv)
			if err != nil {
				t.Fatal(err)
			}
			got := BuildPlan("s", f, noFiles, repo)
			if len(got.Problems) != 1 || !strings.Contains(got.Problems[0], tc.want) {
				t.Errorf("problems: %v", got.Problems)
			}
		})
	}

	t.Run("a pasted file has no repository", func(t *testing.T) {
		f, err := Parse([]byte("services:\n  a:\n    image: x\n    volumes: [\"./data:/data\", \"./app.conf:/app.conf\"]\n"), noEnv)
		if err != nil {
			t.Fatal(err)
		}
		got := BuildPlan("s", f, noFiles, nil)
		if len(got.Problems) != 1 || !strings.Contains(got.Problems[0], "./app.conf") {
			t.Errorf("problems: %v", got.Problems)
		}
	})

	t.Run("a shared folder is still a shared volume", func(t *testing.T) {
		f, err := Parse([]byte("services:\n  a:\n    image: x\n    ports: [\"80\"]\n    volumes: [\"./data:/data\"]\n  b:\n    image: y\n    ports: [\"81\"]\n    volumes: [\"./data:/data\"]\n"), noEnv)
		if err != nil {
			t.Fatal(err)
		}
		got := BuildPlan("s", f, noFiles, repo)
		if len(got.Problems) != 1 || !strings.Contains(got.Problems[0], "is shared by") {
			t.Errorf("problems: %v", got.Problems)
		}
	})
}
