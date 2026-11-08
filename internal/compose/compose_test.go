package compose

import (
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/nuelScript/skiff/internal/config"
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
	if s.Kind != KindApp || s.Public || s.Port != 7700 || len(s.Volumes) != 1 || s.Volumes[0] != (PlanVolume{Name: "search", Path: "/meili_data"}) {
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
		"database volume": {"services:\n  a:\n    image: x\n    volumes: [v:/a]\n  b:\n    image: postgres:16\n    volumes: [v:/var/lib/postgresql/data]\n", "managed database"},
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
		if got := stockEngine(Service{Image: image}); got != want {
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
	wantFiles := []PlanFile{{Source: "./nginx.conf", Target: "/etc/nginx/nginx.conf"}, {Source: "./site", Target: "/www"}, {Source: "./seed", Target: "/seed"}}
	if !slices.Equal(a.Files, wantFiles) {
		t.Errorf("files: %v", a.Files)
	}
	wantVolumes := []PlanVolume{{Name: "seed", Path: "/seed"}, {Name: "data", Path: "/data"}, {Name: "opt-stuff", Path: "/stuff"}}
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

	t.Run("a shared folder is a shared volume", func(t *testing.T) {
		f, err := Parse([]byte("services:\n  a:\n    image: x\n    ports: [\"80\"]\n    volumes: [\"./data:/data\"]\n  b:\n    image: y\n    ports: [\"81\"]\n    volumes: [\"./data:/other\"]\n"), noEnv)
		if err != nil {
			t.Fatal(err)
		}
		got := BuildPlan("s", f, noFiles, repo)
		if len(got.Problems) != 0 {
			t.Fatalf("problems: %v", got.Problems)
		}
		a, b := item(t, got, "a"), item(t, got, "b")
		if a.Volumes[0] != (PlanVolume{Name: "data", Path: "/data"}) || b.Volumes[0] != (PlanVolume{Name: "data", Path: "/other", Owner: "a"}) {
			t.Errorf("a: %+v, b: %+v", a.Volumes, b.Volumes)
		}
	})
}

func TestRunSettings(t *testing.T) {
	const file = `services:
  a:
    image: x
    ports: ["80"]
    entrypoint: ["sh", "-c", "exec server"]
    user: "1000:1000"
    working_dir: /srv
    init: true
    read_only: true
    shm_size: 256m
    tmpfs: [/tmp, /run]
    ulimits:
      nofile: {soft: 1024, hard: 2048}
      nproc: 512
    extra_hosts: ["db.local:10.0.0.1"]
    stop_grace_period: 1m30s
    stop_signal: SIGQUIT
    depends_on: [tools]
  tools:
    image: y
    profiles: [debug]
`
	f, err := Parse([]byte(file), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	p := BuildPlan("s", f, noFiles, nil)
	if len(p.Problems) != 0 {
		t.Fatalf("problems: %v", p.Problems)
	}
	if len(p.Items) != 1 {
		t.Fatalf("a service behind a profile should be left out: %+v", p.Items)
	}
	got := p.Items[0].Options
	want := config.RunOptions{
		Entrypoint: []string{"sh", "-c", "exec server"}, User: "1000:1000", WorkingDir: "/srv", Init: true, ReadOnly: true, ShmSize: "256m",
		Tmpfs: []string{"/tmp", "/run"}, Ulimits: []string{"nofile=1024:2048", "nproc=512"}, ExtraHosts: []string{"db.local:10.0.0.1"},
		StopSeconds: 90, StopSignal: "SIGQUIT",
	}
	if !got.Equal(want) {
		t.Errorf("options: %+v", got)
	}
	for _, note := range p.Notes {
		if strings.Contains(note, "is ignored") {
			t.Errorf("nothing here should be ignored: %s", note)
		}
	}

	t.Run("a custom entrypoint isn't a worker", func(t *testing.T) {
		f, err := Parse([]byte("services:\n  a:\n    image: x\n    ports: [\"80\"]\n  b:\n    image: x\n    command: run\n    entrypoint: other\n"), noEnv)
		if err != nil {
			t.Fatal(err)
		}
		if got := item(t, BuildPlan("s", f, noFiles, nil), "b"); got.Kind != KindApp {
			t.Errorf("kind: %s", got.Kind)
		}
	})

	t.Run("merge tags read as plain values", func(t *testing.T) {
		f, err := Parse([]byte("services:\n  a:\n    image: x\n    deploy: !reset null\n    environment: !override {A: b}\n"), noEnv)
		if err != nil {
			t.Fatal(err)
		}
		if got := f.Services["a"].Environment["A"]; got != "b" {
			t.Errorf("environment: %q", got)
		}
	})

	for name, bad := range map[string]string{
		"a user that looks like a flag": "user: --privileged",
		"a ulimit Skiff doesn't set":    "ulimits: {rtprio: 99}",
		"a relative working folder":     "working_dir: srv",
		"a relative tmpfs":              "tmpfs: [tmp]",
	} {
		t.Run(name, func(t *testing.T) {
			f, err := Parse([]byte("services:\n  a:\n    image: x\n    "+bad+"\n"), noEnv)
			if err != nil {
				t.Fatal(err)
			}
			if got := BuildPlan("s", f, noFiles, nil); len(got.Problems) != 1 {
				t.Errorf("problems: %v", got.Problems)
			}
		})
	}
}

func TestComposeSpellings(t *testing.T) {
	const file = `services:
  web:
    image: x
    ports: ["1025:1025", "8025:8025"]
    mem_limit: 4G
    shm_size: 128mb
    volumes: ["data:/var/lib/data/"]
  cache:
    image: valkey/valkey
    ports: ["6379:6379"]
    deploy: {resources: {limits: {memory: 268435456}}}
    volumes: [{type: volume, source: cache, target: /data/}]
volumes: {data: {}, cache: {}}
`
	p := plan(t, file)
	if len(p.Problems) != 0 {
		t.Fatalf("problems: %v", p.Problems)
	}
	web, cache := item(t, p, "web"), item(t, p, "cache")
	if web.Memory != "4096m" || web.Options.ShmSize != "128m" || cache.Memory != "256m" {
		t.Errorf("sizes: %q %q %q", web.Memory, web.Options.ShmSize, cache.Memory)
	}
	if web.Volumes[0].Path != "/var/lib/data" || cache.Volumes[0].Path != "/data" {
		t.Errorf("paths: %+v %+v", web.Volumes, cache.Volumes)
	}
	if web.Port != 8025 || !web.Public {
		t.Errorf("web should serve its web port: %d %v", web.Port, web.Public)
	}
	if cache.Port != 6379 || cache.Public {
		t.Errorf("a queue port should stay inside the stack: %d %v", cache.Port, cache.Public)
	}
	if got := dockerSize("lots"); got != "lots" {
		t.Errorf("an unreadable size should be left for validation: %q", got)
	}
}

func TestSecretsAndConfigs(t *testing.T) {
	const file = `services:
  app:
    image: x
    ports: ["80"]
    secrets: [key, {source: key, target: other-name}, from-env, outside, missing, undefined]
    configs: [conf, {source: inline, target: /etc/app/inline.conf}, fromvar]
secrets:
  key: {file: ./secrets/key.txt}
  from-env: {environment: TOKEN}
  outside: {external: true}
  missing: {file: ./nope.txt}
configs:
  conf: {file: ./conf/app.conf}
  inline: {content: "a = 1"}
  fromvar: {environment: GREETING}
`
	f, err := Parse([]byte(file), func(k string) (string, bool) { return map[string]string{"GREETING": "hi"}[k], k == "GREETING" })
	if err != nil {
		t.Fatal(err)
	}
	repo := func(p string) PathKind {
		if p == "./secrets/key.txt" || p == "./conf/app.conf" {
			return PathFile
		}
		return PathMissing
	}
	p := BuildPlan("s", f, noFiles, repo)
	a := item(t, p, "app")
	var got []string
	for _, pf := range a.Files {
		if pf.Content != nil {
			got = append(got, pf.Target+"="+*pf.Content)
		} else {
			got = append(got, pf.Target+"<"+pf.Source)
		}
	}
	want := []string{"/run/secrets/key<./secrets/key.txt", "/run/secrets/other-name<./secrets/key.txt", "/conf<./conf/app.conf", "/etc/app/inline.conf=a = 1", "/fromvar=hi"}
	if !slices.Equal(got, want) {
		t.Errorf("files:\n got %v\nwant %v", got, want)
	}
	for _, why := range []string{"secret from-env comes from an environment variable", "secret outside is external", "secret missing comes from ./nope.txt, which isn't in the repository", "secret undefined isn't defined"} {
		if !slices.ContainsFunc(p.Problems, func(s string) bool { return strings.Contains(s, why) }) {
			t.Errorf("no problem saying %q in %v", why, p.Problems)
		}
	}

	t.Run("a pasted file has no repository to read a secret from", func(t *testing.T) {
		f, err := Parse([]byte("services:\n  a:\n    image: x\n    secrets: [k]\nsecrets:\n  k: {file: ./k}\n"), noEnv)
		if err != nil {
			t.Fatal(err)
		}
		if p := BuildPlan("s", f, noFiles, nil); len(p.Problems) != 1 || !strings.Contains(p.Problems[0], "only works from a repository") {
			t.Errorf("problems: %v", p.Problems)
		}
	})

	t.Run("a database reads its password from a secret file", func(t *testing.T) {
		f, err := Parse([]byte("services:\n  db:\n    image: postgres:16\n    environment: {POSTGRES_PASSWORD_FILE: /run/secrets/pw}\n    secrets: [pw]\nsecrets:\n  pw: {file: ./pw.txt}\n"), noEnv)
		if err != nil {
			t.Fatal(err)
		}
		read := func(name string) ([]byte, error) {
			if name == "./pw.txt" {
				return []byte("hunter2\n"), nil
			}
			return nil, errors.New("missing")
		}
		if db := item(t, BuildPlan("s", f, read, repo), "db"); db.Kind != KindDatabase || db.DBPassword != "hunter2" {
			t.Errorf("database: %s %q", db.Kind, db.DBPassword)
		}
	})
}

func TestExtends(t *testing.T) {
	const file = `services:
  base:
    image: x
    environment: {A: "1", B: "1"}
    ports: ["80"]
    volumes: ["data:/data"]
    command: serve
  child:
    extends: base
    environment: {B: "2"}
    ports: ["81"]
    command: other
  grandchild:
    extends: {service: child}
    environment: [C=3]
volumes: {data: {}}
`
	f, err := Parse([]byte(file), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	child, grand := f.Services["child"], f.Services["grandchild"]
	if !maps.Equal(child.Environment, map[string]string{"A": "1", "B": "2"}) || !slices.Equal(child.Command, []string{"other"}) {
		t.Errorf("child: %v %v", child.Environment, child.Command)
	}
	if len(child.Ports) != 2 || child.Ports[1].Target != 81 || len(child.Volumes) != 1 || child.Image != "x" {
		t.Errorf("child should keep the base's lists and add its own: %+v", child)
	}
	if grand.Environment["C"] != "3" || grand.Environment["B"] != "2" {
		t.Errorf("an environment list should merge with a map: %v", grand.Environment)
	}
	if len(f.Extra["child"]) != 0 {
		t.Errorf("extends shouldn't be left as an unread key: %v", f.Extra)
	}

	if _, err := Parse([]byte("services:\n  a: {extends: b, image: x}\n  b: {extends: a, image: x}\n"), noEnv); err == nil || !strings.Contains(err.Error(), "circle") {
		t.Errorf("a circle: %v", err)
	}
	if _, err := Parse([]byte("services:\n  a: {extends: nope, image: x}\n"), noEnv); err == nil {
		t.Error("extending a service that isn't there should be refused")
	}
	other, err := Parse([]byte("services:\n  a:\n    image: x\n    extends: {file: other.yml, service: b}\n"), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if p := BuildPlan("s", other, noFiles, nil); !slices.ContainsFunc(p.Problems, func(s string) bool { return strings.Contains(s, "from another file") }) {
		t.Errorf("extends from another file: %v", p.Problems)
	}
}

func TestMergeKeys(t *testing.T) {
	f, err := Parse([]byte(`
x-shared: &shared
  image: worker:1
  restart: always
  privileged: true
x-env: &env
  DATABASE_URL: ${DATABASE_URL:-postgres://db/app}
  MODE: shared
x-more: &more
  MODE: more
  EXTRA: "1"
services:
  web:
    image: web:1
    ports: ["3000:3000"]
    environment:
      <<: [*env, *more]
      OWN: yes
  worker:
    <<: *shared
    environment:
      <<: *env
      MODE: own
  literal:
    image: lit:1
    environment:
      "<<": not-a-merge
`), func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	web := f.Services["web"].Environment
	if web["DATABASE_URL"] != "postgres://db/app" || web["MODE"] != "shared" || web["EXTRA"] != "1" || web["OWN"] != "yes" {
		t.Errorf("web environment = %v", web)
	}
	worker := f.Services["worker"]
	if worker.Image != "worker:1" || worker.Environment["MODE"] != "own" || worker.Environment["DATABASE_URL"] != "postgres://db/app" {
		t.Errorf("worker = %+v", worker)
	}
	if !slices.Contains(f.Extra["worker"], "privileged") {
		t.Errorf("a key merged into a service isn't checked: %v", f.Extra["worker"])
	}
	if f.Services["literal"].Environment["<<"] != "not-a-merge" {
		t.Errorf("a quoted << key = %v", f.Services["literal"].Environment)
	}
}

func TestDatabaseWithSetupScripts(t *testing.T) {
	f, err := Parse([]byte(`
services:
  postgres:
    image: postgres:16
    environment: {POSTGRES_PASSWORD: secret}
    volumes:
      - db:/var/lib/postgresql/data
      - ./init-data.sh:/docker-entrypoint-initdb.d/init-data.sh
  plain:
    image: postgres:16
    environment: {POSTGRES_PASSWORD: secret}
volumes:
  db:
`), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	p := BuildPlan("shop", f, noFiles, func(path string) PathKind {
		if path == "./init-data.sh" {
			return PathFile
		}
		return PathMissing
	})
	if db := item(t, p, "postgres"); db.Kind != KindApp || len(db.Files) != 1 {
		t.Errorf("a database with setup scripts = %+v", db)
	}
	if db := item(t, p, "plain"); db.Kind != KindDatabase {
		t.Errorf("a stock database = %+v", db)
	}
	if !slices.ContainsFunc(p.Notes, func(n string) bool { return strings.Contains(n, "setup scripts") }) {
		t.Errorf("notes = %v", p.Notes)
	}
}

func TestAliases(t *testing.T) {
	p := plan(t, `
x-db-credentials: &db-credentials
  POSTGRES_USER: &db-user listmonk
  POSTGRES_PASSWORD: &db-password secret
  POSTGRES_DB: &db-name listmonk
x-command: &cmd ["./serve", "--port", "9000"]
services:
  app:
    image: app:1
    ports: ["9000:9000"]
    command: *cmd
    environment:
      DB_USER: *db-user
      DB_PASSWORD: *db-password
      DB_NAME: *db-name
  db:
    image: postgres:17-alpine
    environment:
      <<: *db-credentials
`)
	app := item(t, p, "app")
	if app.Env["DB_USER"] != "listmonk" || app.Env["DB_PASSWORD"] != "secret" || app.Env["DB_NAME"] != "listmonk" {
		t.Errorf("an alias in environment gave the anchor's name, not its value: %v", app.Env)
	}
	if strings.Join(app.Command, " ") != "./serve --port 9000" {
		t.Errorf("command = %v", app.Command)
	}
	if db := item(t, p, "db"); db.DBUser != "listmonk" || db.DBPassword != "secret" || db.DB != "listmonk" {
		t.Errorf("database credentials = %q %q %q", db.DBUser, db.DBPassword, db.DB)
	}
}

func TestDatabasesWithoutPasswords(t *testing.T) {
	p := plan(t, `
services:
  pg-trust:
    image: postgres:15
    environment: {POSTGRES_DB: db, POSTGRES_HOST_AUTH_METHOD: trust}
  mysql-open:
    image: mysql:8
    environment: {MYSQL_ALLOW_EMPTY_PASSWORD: "yes", MYSQL_DATABASE: app}
  mongo-open:
    image: mongo:7
  mongo-auth:
    image: mongo:7
    environment: {MONGO_INITDB_ROOT_USERNAME: root, MONGO_INITDB_ROOT_PASSWORD: secret}
  pg:
    image: postgres:15
    environment: {POSTGRES_PASSWORD: secret}
`)
	for _, name := range []string{"pg-trust", "mysql-open", "mongo-open"} {
		if it := item(t, p, name); it.Kind != KindApp {
			t.Errorf("%s has no password but became %s", name, it.Kind)
		}
		if !slices.ContainsFunc(p.Notes, func(n string) bool { return strings.HasPrefix(n, name+": lets apps in without a password") }) {
			t.Errorf("no note for %s: %v", name, p.Notes)
		}
	}
	for _, name := range []string{"mongo-auth", "pg"} {
		if it := item(t, p, name); it.Kind != KindDatabase {
			t.Errorf("%s = %s, want a managed database", name, it.Kind)
		}
	}
}
