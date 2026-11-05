//go:build e2e

package e2e

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nuelScript/skiff/internal/db"
)

const stackFile = `
services:
  api:
    image: python:3.12-alpine
    command: python -m http.server 8000
    ports: ["8080:8000"]
    environment:
      DATABASE_URL: postgres://app:${DB_PASSWORD}@db:5432/shop
    depends_on: [db, cache]
  worker:
    image: python:3.12-alpine
    command: python -c "import time; time.sleep(100000)"
    environment:
      DATABASE_URL: postgres://app:${DB_PASSWORD}@db:5432/shop
    depends_on: [db]
  probe:
    image: alpine:3.20
    command: sleep 100000
    volumes: [files:/files]
    environment: {MARK: "%s"}
  db:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: app
      POSTGRES_PASSWORD: ${DB_PASSWORD}
      POSTGRES_DB: shop
    volumes: [pg:/var/lib/postgresql/data]
  cache:
    image: redis:7-alpine
volumes: {files: {}, pg: {}}
`

type stackMember struct{ Service, App, Kind, Engine, State string }

// TestComposeStack creates real stacks from a Compose file and checks services find each other the way the file expects.
func TestComposeStack(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	bin := filepath.Join(root, "skiff")
	run(t, "", nil, "go", "build", "-o", bin, "..")
	protectExisting(t, home)
	panel := freeAddr(t)
	start(t, []string{"HOME=" + home, "SKIFF_PANEL_PASSWORD=e2e-setup"}, bin, "panel", "--addr", panel, "--domain", "localhost")
	waitHTTP(t, "http://"+panel+"/api/me")
	c := client(t)
	post(t, c, "http://"+panel+"/api/auth/setup", `{"secret":"e2e-setup","email":"e2e@example.com","name":"E2E","password":"e2e-password-1"}`)

	base := "http://" + panel + "/api/stacks"
	request := func(name, mark string, vars bool) string {
		v := `{}`
		if vars {
			v = `{"DB_PASSWORD":"s3cret-` + name + `"}`
		}
		body := fmt.Sprintf(stackFile, mark)
		quoted := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(body)
		return fmt.Sprintf(`{"name":%q,"compose":"%s","vars":%s}`, name, quoted, v)
	}
	cleanup := func(name string) {
		req, _ := http.NewRequest(http.MethodDelete, base+"?name="+name, nil)
		if resp, err := c.Do(req); err == nil {
			resp.Body.Close()
		}
	}
	t.Cleanup(func() { cleanup("e2e-a"); cleanup("e2e-b") })

	// The plan asks for the missing variable and never shows values.
	var plan struct {
		Items []struct{ Service, Kind, WorkerOf, Engine string }
		Unset []string
	}
	postJSON(t, c, base+"/plan", request("e2e-a", "a", false), &plan)
	if len(plan.Unset) != 1 || plan.Unset[0] != "DB_PASSWORD" {
		t.Fatalf("unset: %+v", plan.Unset)
	}
	resp, err := c.Post(base+"/plan", "application/json", strings.NewReader(request("e2e-a", "a", true)))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.Contains(string(raw), "s3cret") {
		t.Fatalf("the plan leaks a secret: %s", raw)
	}

	for _, s := range []struct{ name, mark string }{{"e2e-a", "a"}, {"e2e-b", "b"}} {
		resp, err := c.Post(base, "application/json", strings.NewReader(request(s.name, s.mark, true)))
		if err != nil {
			t.Fatal(err)
		}
		msg, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("creating %s: %d %s", s.name, resp.StatusCode, msg)
		}
	}

	members := func(name string) []stackMember {
		var list []struct {
			Name    string
			Members []stackMember
		}
		getJSON(t, c, base, &list)
		for _, s := range list {
			if s.Name == name {
				return s.Members
			}
		}
		return nil
	}
	for _, name := range []string{"e2e-a", "e2e-b"} {
		eventually(t, 4*time.Minute, func() bool {
			m := members(name)
			if len(m) != 5 {
				return false
			}
			for _, x := range m {
				if x.State != "running" {
					return false
				}
			}
			return true
		})
	}

	inProbe := func(stack, script string) string {
		t.Helper()
		out, err := exec.Command("docker", "ps", "-q", "--filter", "label=skiff.app="+stack+"-probe").Output()
		if err != nil || strings.TrimSpace(string(out)) == "" {
			t.Fatalf("no probe container for %s: %v", stack, err)
		}
		return dockerExec(t, strings.Fields(string(out))[0], script)
	}
	dbContainer := func(stack, service string) string {
		t.Helper()
		out, err := exec.Command("docker", "ps", "--filter", "label=skiff.stack="+stack, "--filter", "label=skiff.kind=database", "--format", "{{.Names}}").Output()
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range strings.Fields(string(out)) {
			img, _ := exec.Command("docker", "inspect", "-f", "{{.Config.Image}}", n).Output()
			if strings.HasPrefix(string(img), map[string]string{"db": "postgres", "cache": "redis"}[service]) {
				return n
			}
		}
		t.Fatalf("no %s for %s in %s", service, stack, out)
		return ""
	}

	for _, stack := range []string{"e2e-a", "e2e-b"} {
		// Compose service names resolve, on the file's own ports.
		for _, target := range []string{"db 5432", "cache 6379", "api 8000"} {
			inProbe(stack, "nc -z -w 5 "+target)
		}
		if got := inProbe(stack, "wget -qO- http://api:8000/ | head -c 200"); !strings.Contains(strings.ToLower(got), "directory listing") {
			t.Fatalf("%s: api answered %q", stack, got)
		}
		// The file's credentials work, over the service name.
		db := dbContainer(stack, "db")
		if got := dockerExec(t, db, "PGPASSWORD=s3cret-"+stack+" psql -h db -U app -d shop -tAc 'select 1'"); got != "1" {
			t.Fatalf("%s: psql over the service name: %q", stack, got)
		}
		// Redis has no password in the file, so clients without one must work.
		if got := dockerExec(t, dbContainer(stack, "cache"), "redis-cli -h cache ping"); got != "PONG" {
			t.Fatalf("%s: redis: %q", stack, got)
		}
		// The worker shares the API's image and env, and runs its own command.
		out, _ := exec.Command("docker", "ps", "-q", "--filter", "label=skiff.worker.app="+stack+"-api").Output()
		ids := strings.Fields(string(out))
		if len(ids) != 1 {
			t.Fatalf("%s: workers: %q", stack, ids)
		}
		if got := dockerExec(t, ids[0], "echo $DATABASE_URL"); got != "postgres://app:s3cret-"+stack+"@db:5432/shop" {
			t.Fatalf("%s: worker env: %q", stack, got)
		}
	}

	// The same service names in two stacks stay apart.
	marker := func(stack, value string) {
		t.Helper()
		dockerExec(t, dbContainer(stack, "db"), "PGPASSWORD=s3cret-"+stack+" psql -h db -U app -d shop -qc \"create table who(v text); insert into who values ('"+value+"')\"")
	}
	marker("e2e-a", "first")
	marker("e2e-b", "second")
	got := dockerExec(t, dbContainer("e2e-b", "db"), "PGPASSWORD=s3cret-e2e-b psql -h db -U app -d shop -tAc 'select v from who'")
	if got != "second" {
		t.Fatalf("stack b reached the wrong database: %q", got)
	}
	netA, _ := exec.Command("docker", "inspect", "-f", "{{range $k, $v := .NetworkSettings.Networks}}{{$k}} {{end}}", dbContainer("e2e-a", "db")).Output()
	if strings.TrimSpace(string(netA)) != "skiff-s-e2e-a" {
		t.Fatalf("database networks: %q", netA)
	}

	// Backups and restore work on a stack's database.
	var dbs []struct{ ID, Name string }
	getJSON(t, c, "http://"+panel+"/api/databases", &dbs)
	id := ""
	for _, d := range dbs {
		if d.Name == "e2e-a-db" {
			id = d.ID
		}
	}
	if id == "" {
		t.Fatalf("stack database isn't listed: %+v", dbs)
	}
	var b struct{ ID string }
	postJSON(t, c, "http://"+panel+"/api/backups?db="+id, "", &b)
	a := dbContainer("e2e-a", "db")
	dockerExec(t, a, "PGPASSWORD=s3cret-e2e-a psql -h db -U app -d shop -qc \"update who set v='changed'\"")
	post(t, c, "http://"+panel+"/api/backups/restore?id="+b.ID, "")
	if got := dockerExec(t, a, "PGPASSWORD=s3cret-e2e-a psql -h db -U app -d shop -tAc 'select v from who'"); got != "first" {
		t.Fatalf("restore on a stack database: %q", got)
	}

	// A taken name is refused.
	resp, err = c.Post(base, "application/json", strings.NewReader(request("e2e-a", "a", true)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("a duplicate stack: %d", resp.StatusCode)
	}

	// Deleting leaves nothing behind, and doesn't touch the other stack.
	cleanup("e2e-a")
	eventually(t, time.Minute, func() bool {
		out, _ := exec.Command("sh", "-c", "docker ps -aq --filter label=skiff.stack=e2e-a; docker ps -aq --filter label=skiff.app=e2e-a-api; docker ps -aq --filter label=skiff.app=e2e-a-probe; docker ps -aq --filter label=skiff.worker.app=e2e-a-api; docker network ls -q --filter name=skiff-s-e2e-a; docker volume ls -q --filter name=skiff-e2e-a-probe").Output()
		return strings.TrimSpace(string(out)) == ""
	})
	if m := members("e2e-b"); len(m) != 5 {
		t.Fatalf("deleting one stack changed another: %+v", m)
	}
	inProbe("e2e-b", "nc -z -w 5 db 5432")
}

// TestComposeStackFromRepo builds a stack from a repository, with the Compose file in a subfolder and a custom Dockerfile.
func TestComposeStackFromRepo(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	bin := filepath.Join(root, "skiff")
	run(t, "", nil, "go", "build", "-o", bin, "..")
	repo, push := gitServer(t, root)
	work := filepath.Join(root, "work")
	for path, content := range map[string]string{
		"api/Dockerfile.custom": "FROM busybox:1.36\nRUN echo built-custom > /marker && mkdir /www && echo hello-from-stack > /www/index.html\nCMD [\"httpd\", \"-f\", \"-p\", \"3000\", \"-h\", \"/www\"]\n",
		"deploy/compose.yml": `services:
  web:
    build: {context: ../api, dockerfile: Dockerfile.custom}
    ports: ["3000"]
    env_file: app.env
    environment: {FROM_DOTENV: "${FROM_DOTENV}", FROM_REQUEST: "${FROM_REQUEST}"}
  jobs:
    build: {context: ../api, dockerfile: Dockerfile.custom}
    command: sleep 100000
    env_file: app.env
    environment: {FROM_DOTENV: "${FROM_DOTENV}", FROM_REQUEST: "${FROM_REQUEST}"}
`,
		"deploy/.env":    "FROM_DOTENV=dotenv-value\nFROM_REQUEST=dotenv-loses\n",
		"deploy/app.env": "FROM_ENV_FILE=file-value\n",
	} {
		full := filepath.Join(work, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	push("v1")

	protectExisting(t, home)
	panel := freeAddr(t)
	start(t, []string{"HOME=" + home, "SKIFF_PANEL_PASSWORD=e2e-setup", "SKIFF_ALLOW_PRIVATE_ENDPOINTS=1"}, bin, "panel", "--addr", panel, "--domain", "localhost")
	waitHTTP(t, "http://"+panel+"/api/me")
	c := client(t)
	post(t, c, "http://"+panel+"/api/auth/setup", `{"secret":"e2e-setup","email":"e2e@example.com","name":"E2E","password":"e2e-password-1"}`)

	base := "http://" + panel + "/api/stacks"
	t.Cleanup(func() {
		req, _ := http.NewRequest(http.MethodDelete, base+"?name=e2e-repo", nil)
		if resp, err := c.Do(req); err == nil {
			resp.Body.Close()
		}
	})
	body := fmt.Sprintf(`{"name":"e2e-repo","git":%q,"path":"deploy/compose.yml","vars":{"FROM_REQUEST":"request-value"}}`, repo)
	resp, err := c.Post(base, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("creating the stack: %d %s", resp.StatusCode, msg)
	}

	var ctr string
	eventually(t, 3*time.Minute, func() bool {
		out, _ := exec.Command("docker", "ps", "-q", "--filter", "label=skiff.app=e2e-repo-web").Output()
		ids := strings.Fields(string(out))
		if len(ids) != 1 {
			return false
		}
		ctr = ids[0]
		out, _ = exec.Command("docker", "ps", "-q", "--filter", "label=skiff.worker.app=e2e-repo-web").Output()
		return len(strings.Fields(string(out))) == 1
	})
	if got := dockerExec(t, ctr, "cat /marker"); got != "built-custom" {
		t.Fatalf("the custom Dockerfile wasn't used: %q", got)
	}
	for k, want := range map[string]string{"FROM_DOTENV": "dotenv-value", "FROM_REQUEST": "request-value", "FROM_ENV_FILE": "file-value"} {
		if got := dockerExec(t, ctr, "printenv "+k); got != want {
			t.Fatalf("%s = %q, want %q", k, got, want)
		}
	}
	if got := dockerExec(t, ctr, "wget -qO- http://web:3000/"); got != "hello-from-stack" {
		t.Fatalf("over its own service name: %q", got)
	}

	var tok struct{ Token string }
	postJSON(t, c, "http://"+panel+"/api/tokens", `{"name":"e2e-stack"}`, &tok)
	req, _ := http.NewRequest(http.MethodPost, "http://"+panel+"/api/v1/apps/e2e-repo-web/previews", strings.NewReader(`{"branch":"feat/login"}`))
	req.Header.Set("Authorization", "Bearer "+tok.Token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("a preview of a stack service through the API: %d", resp.StatusCode)
	}
	// Pushing a changed Compose file shows up as a change, and applying it reaches the app.
	file := filepath.Join(work, "deploy", "compose.yml")
	current, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(strings.Replace(string(current), `environment: {FROM_DOTENV: "${FROM_DOTENV}", FROM_REQUEST: "${FROM_REQUEST}"}
  jobs:`, `environment: {FROM_DOTENV: "${FROM_DOTENV}", FROM_REQUEST: "${FROM_REQUEST}", PUSHED: "later"}
  jobs:`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	push("v2")
	update := `{"name":"e2e-repo"}`
	resp, err = c.Post(base+"/changes", "application/json", strings.NewReader(update))
	if err != nil {
		t.Fatal(err)
	}
	msg, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(msg), "adds env: PUSHED") || !strings.Contains(string(msg), `"problems":[]`) {
		t.Fatalf("changes after a push: %s", msg)
	}
	resp, err = c.Post(base+"/update", "application/json", strings.NewReader(update))
	if err != nil {
		t.Fatal(err)
	}
	msg, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("update from the repository: %d %s", resp.StatusCode, msg)
	}
	eventually(t, 2*time.Minute, func() bool {
		out, _ := exec.Command("sh", "-c", "docker exec $(docker ps -q --filter label=skiff.app=e2e-repo-web) printenv PUSHED").Output()
		return strings.TrimSpace(string(out)) == "later"
	})
	out, _ := exec.Command("docker", "ps", "-q", "--filter", "label=skiff.app=e2e-repo-web").Output()
	ctr = strings.TrimSpace(string(out))

	// Nothing secret reaches the image.
	if out, _ := exec.Command("docker", "exec", ctr, "ls", "/app/skiff.toml").CombinedOutput(); !strings.Contains(string(out), "No such file") && !strings.Contains(string(out), "can't access") {
		t.Fatalf("skiff.toml in the image: %s", out)
	}
}

const updateV1 = `
services:
  api:
    image: python:3.12-alpine
    command: python -m http.server 8000
    ports: ["8080:8000"]
    environment: {DATABASE_URL: "postgres://app:${DB_PASSWORD}@db:5432/shop"}
  worker:
    image: python:3.12-alpine
    command: python -c "import time; time.sleep(100000)"
    environment: {DATABASE_URL: "postgres://app:${DB_PASSWORD}@db:5432/shop"}
  probe:
    image: alpine:3.20
    command: sleep 100000
    volumes: [files:/files]
    environment: {MARK: one}
  extra:
    image: alpine:3.20
    command: sleep 100000
    volumes: [scratch:/scratch]
  db:
    image: postgres:16-alpine
    environment: {POSTGRES_USER: app, POSTGRES_PASSWORD: "${DB_PASSWORD}", POSTGRES_DB: shop}
`

const updateV2 = `
services:
  api:
    image: python:3.12-alpine
    command: python -m http.server 8000
    ports: ["8080:8000"]
    environment: {DATABASE_URL: "postgres://app:${DB_PASSWORD}@db:5432/shop"}
  worker:
    image: python:3.12-alpine
    command: python -c "import time; time.sleep(200000)"
    environment: {DATABASE_URL: "postgres://app:${DB_PASSWORD}@db:5432/shop"}
  probe:
    image: alpine:3.20
    command: sleep 100000
    volumes: [files:/files]
    environment: {MARK: two, ADDED: "yes"}
  fresh:
    image: alpine:3.20
    command: sleep 100000
  db:
    image: postgres:16-alpine
    environment: {POSTGRES_USER: app, POSTGRES_PASSWORD: "${DB_PASSWORD}", POSTGRES_DB: shop}
`

// TestComposeStackUpdate changes a running stack from its file and checks only what changed is touched.
func TestComposeStackUpdate(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	bin := filepath.Join(root, "skiff")
	run(t, "", nil, "go", "build", "-o", bin, "..")
	protectExisting(t, home)
	panel := freeAddr(t)
	start(t, []string{"HOME=" + home, "SKIFF_PANEL_PASSWORD=e2e-setup"}, bin, "panel", "--addr", panel, "--domain", "localhost")
	waitHTTP(t, "http://"+panel+"/api/me")
	c := client(t)
	post(t, c, "http://"+panel+"/api/auth/setup", `{"secret":"e2e-setup","email":"e2e@example.com","name":"E2E","password":"e2e-password-1"}`)

	base := "http://" + panel + "/api/stacks"
	const stack = "e2e-up"
	t.Cleanup(func() {
		req, _ := http.NewRequest(http.MethodDelete, base+"?name="+stack, nil)
		if resp, err := c.Do(req); err == nil {
			resp.Body.Close()
		}
	})
	body := func(file, extra string) string {
		quoted := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(file)
		return fmt.Sprintf(`{"name":%q,"compose":"%s"%s}`, stack, quoted, extra)
	}
	send := func(url, b string) (int, string) {
		t.Helper()
		resp, err := c.Post(url, "application/json", strings.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		msg, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(msg)
	}
	ctr := func(service string) string {
		out, _ := exec.Command("docker", "ps", "-q", "--filter", "label=skiff.app="+stack+"-"+service).Output()
		return strings.TrimSpace(string(out))
	}
	dbCtr := func() string {
		out, _ := exec.Command("docker", "ps", "-q", "--filter", "label=skiff.stack="+stack, "--filter", "label=skiff.kind=database").Output()
		return strings.TrimSpace(string(out))
	}

	if code, msg := send(base, body(updateV1, `,"vars":{"DB_PASSWORD":"keep-me-secret"}`)); code != http.StatusCreated {
		t.Fatalf("create: %d %s", code, msg)
	}
	workerCtr := func() string {
		out, _ := exec.Command("docker", "ps", "-q", "--filter", "label=skiff.worker.app="+stack+"-api").Output()
		return strings.TrimSpace(string(out))
	}
	eventually(t, 3*time.Minute, func() bool {
		return ctr("api") != "" && ctr("probe") != "" && ctr("extra") != "" && dbCtr() != "" && workerCtr() != ""
	})
	dockerExec(t, ctr("probe"), "echo kept > /files/note")
	apiBefore, dbBefore, workerBefore := ctr("api"), dbCtr(), workerCtr()

	// The same file means no changes, and the password is remembered rather than asked for again.
	code, msg := send(base+"/changes", body(updateV1, ""))
	if code != 200 || !strings.Contains(msg, `"changes":[]`) || !strings.Contains(msg, `"unset":[]`) || !strings.Contains(msg, `"problems":[]`) {
		t.Fatalf("no changes expected: %d %s", code, msg)
	}

	code, msg = send(base+"/changes", body(updateV2, ""))
	if code != 200 || strings.Contains(msg, "keep-me-secret") {
		t.Fatalf("changes: %d %s", code, msg)
	}
	for _, want := range []string{`"service":"fresh"`, `"action":"add"`, `"service":"extra"`, `"action":"remove"`, `"service":"probe"`, "adds env: ADDED", "changes env: MARK", `"service":"worker"`, `"deletesData":true`} {
		if !strings.Contains(msg, want) {
			t.Errorf("changes missing %s: %s", want, msg)
		}
	}
	if strings.Contains(msg, `"service":"api"`) || strings.Contains(msg, `"service":"db"`) {
		t.Errorf("unchanged services listed: %s", msg)
	}

	// Deleting data needs an explicit yes.
	if code, _ := send(base+"/update", body(updateV2, "")); code != http.StatusUnprocessableEntity {
		t.Fatalf("an update that deletes data went through without permission: %d", code)
	}
	if ctr("extra") == "" {
		t.Fatal("a refused update still changed the stack")
	}
	if code, msg := send(base+"/update", body(updateV2, `,"allowDataLoss":true`)); code != 200 {
		t.Fatalf("update: %d %s", code, msg)
	}
	eventually(t, 2*time.Minute, func() bool {
		p := ctr("probe")
		if p == "" || ctr("fresh") == "" || ctr("extra") != "" || workerCtr() == workerBefore || workerCtr() == "" {
			return false
		}
		out, _ := exec.Command("docker", "exec", p, "printenv", "MARK").Output()
		return strings.TrimSpace(string(out)) == "two"
	})
	if got := dockerExec(t, ctr("probe"), "printenv ADDED; cat /files/note"); got != "yes\nkept" {
		t.Fatalf("probe after the update: %q", got)
	}
	if got := dockerExec(t, workerCtr(), "cat /proc/1/cmdline | tr '\\0' ' '"); !strings.Contains(got, "200000") {
		t.Fatalf("the worker kept its old command: %q", got)
	}
	if ctr("api") != apiBefore {
		t.Error("the update restarted an unchanged app")
	}
	if dbCtr() != dbBefore {
		t.Error("the update restarted the database")
	}
	eventually(t, 30*time.Second, func() bool {
		out, _ := exec.Command("docker", "volume", "ls", "-q", "--filter", "name=skiff-"+stack+"-extra").Output()
		return strings.TrimSpace(string(out)) == ""
	})

	// A database's image or password can't change under its data.
	changed := strings.Replace(updateV2, "postgres:16-alpine", "postgres:15-alpine", 1)
	if code, msg := send(base+"/update", body(changed, `,"allowDataLoss":true`)); code != http.StatusUnprocessableEntity || !strings.Contains(msg, "isn't applied to existing data") {
		t.Fatalf("an image change: %d %s", code, msg)
	}
	if code, msg := send(base+"/update", body(updateV2, `,"vars":{"DB_PASSWORD":"different"},"allowDataLoss":true`)); code != http.StatusUnprocessableEntity || !strings.Contains(msg, "changing the password") {
		t.Fatalf("a password change: %d %s", code, msg)
	}
	if dbCtr() != dbBefore {
		t.Error("a refused change restarted the database")
	}

	// Removing the database needs the same yes, then really removes it.
	noDB := updateV2[:strings.Index(updateV2, "  db:\n")]
	if code, _ := send(base+"/update", body(noDB, "")); code != http.StatusUnprocessableEntity {
		t.Fatalf("removing a database without permission: %d", code)
	}
	if code, msg := send(base+"/update", body(noDB, `,"allowDataLoss":true`)); code != 200 {
		t.Fatalf("removing the database: %d %s", code, msg)
	}
	eventually(t, time.Minute, func() bool { return dbCtr() == "" })
}

// TestComposeStackFollowsPushes sends signed GitHub webhooks: safe changes apply on their own, and ones that would delete data wait.
func TestComposeStackFollowsPushes(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	bin := filepath.Join(root, "skiff")
	run(t, "", nil, "go", "build", "-o", bin, "..")
	repo, pushRepo := gitServer(t, root)
	work := filepath.Join(root, "work")
	write := func(path, content string) {
		t.Helper()
		full := filepath.Join(work, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dockerfile := func(page string) string {
		return "FROM busybox:1.36\nRUN mkdir /www && echo " + page + " > /www/index.html\nCMD [\"httpd\", \"-f\", \"-p\", \"3000\", \"-h\", \"/www\"]\n"
	}
	composeFile := func(env string, withVolume, withJobs bool) string {
		f := "services:\n  web:\n    build: ./api\n    ports: [\"3000\"]\n    environment: {NOTE: " + env + "}\n"
		if withVolume {
			f += "    volumes: [data:/data]\n"
		}
		if withJobs {
			f += "  jobs:\n    image: alpine:3.20\n    command: sleep 100000\n"
		}
		return f
	}
	write("api/Dockerfile", dockerfile("page-one"))
	write("compose.yml", composeFile("first", true, false))
	pushRepo("v1")

	protectExisting(t, home)
	if err := os.MkdirAll(filepath.Join(home, ".skiff"), 0o700); err != nil {
		t.Fatal(err)
	}
	const hookSecret = "hook-secret"
	if err := os.WriteFile(filepath.Join(home, ".skiff", "github.json"), []byte(`{"id":1,"pem":"x","webhook_secret":"`+hookSecret+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	panel := freeAddr(t)
	start(t, []string{"HOME=" + home, "SKIFF_PANEL_PASSWORD=e2e-setup", "SKIFF_ALLOW_PRIVATE_ENDPOINTS=1"}, bin, "panel", "--addr", panel, "--domain", "localhost")
	waitHTTP(t, "http://"+panel+"/api/me")
	c := client(t)
	post(t, c, "http://"+panel+"/api/auth/setup", `{"secret":"e2e-setup","email":"e2e@example.com","name":"E2E","password":"e2e-password-1"}`)

	base := "http://" + panel + "/api/stacks"
	const stack = "e2e-hook"
	t.Cleanup(func() {
		req, _ := http.NewRequest(http.MethodDelete, base+"?name="+stack, nil)
		if resp, err := c.Do(req); err == nil {
			resp.Body.Close()
		}
	})
	resp, err := c.Post(base, "application/json", strings.NewReader(fmt.Sprintf(`{"name":%q,"git":%q}`, stack, repo)))
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d %s", resp.StatusCode, msg)
	}
	web := func() string {
		out, _ := exec.Command("docker", "ps", "-q", "--filter", "label=skiff.app="+stack+"-web").Output()
		return strings.TrimSpace(string(out))
	}
	eventually(t, 2*time.Minute, func() bool { return web() != "" })

	// The test server's URL isn't github.com, so give the stack the name a webhook would carry.
	store, err := db.OpenAt(filepath.Join(home, ".skiff", "skiff.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Exec(`UPDATE stacks SET repo='acme/shop', branch='main' WHERE name=?`, stack); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Exec(`UPDATE sources SET repo='acme/shop', branch='main' WHERE stack=?`, stack); err != nil {
		t.Fatal(err)
	}

	hook := func() {
		t.Helper()
		body := []byte(`{"ref":"refs/heads/main","after":"","repository":{"full_name":"acme/shop"},"head_commit":{"message":"change","added":[],"modified":[],"removed":[]}}`)
		mac := hmac.New(sha256.New, []byte(hookSecret))
		mac.Write(body)
		req, _ := http.NewRequest(http.MethodPost, "http://"+panel+"/api/github/hook", bytes.NewReader(body))
		req.Header.Set("X-GitHub-Event", "push")
		req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("webhook: %d", resp.StatusCode)
		}
	}
	env := func(key string) string {
		id := web()
		if id == "" {
			return ""
		}
		out, _ := exec.Command("docker", "exec", id, "printenv", key).Output()
		return strings.TrimSpace(string(out))
	}
	page := func() string {
		id := web()
		if id == "" {
			return ""
		}
		out, _ := exec.Command("docker", "exec", id, "cat", "/www/index.html").Output()
		return strings.TrimSpace(string(out))
	}
	held := func() string {
		var list []struct{ Name, Held string }
		getJSON(t, c, base, &list)
		for _, s := range list {
			if s.Name == stack {
				return s.Held
			}
		}
		return ""
	}
	dockerExec(t, web(), "echo precious > /data/file")

	// A safe change applies from the push alone, including a new service.
	write("compose.yml", composeFile("second", true, true))
	pushRepo("v2")
	hook()
	eventually(t, 2*time.Minute, func() bool {
		out, _ := exec.Command("docker", "ps", "-q", "--filter", "label=skiff.app="+stack+"-jobs").Output()
		return env("NOTE") == "second" && strings.TrimSpace(string(out)) != ""
	})
	if got := dockerExec(t, web(), "cat /data/file"); got != "precious" {
		t.Fatalf("the volume after a push: %q", got)
	}
	if got := held(); got != "" {
		t.Fatalf("a safe change was held: %q", got)
	}

	// A push that would delete a volume waits, while its new code still deploys with the current settings.
	write("api/Dockerfile", dockerfile("page-two"))
	write("compose.yml", composeFile("third", false, true))
	pushRepo("v3")
	hook()
	eventually(t, 2*time.Minute, func() bool { return page() == "page-two" })
	if got := held(); !strings.Contains(got, "delete data in web") {
		t.Fatalf("the push wasn't held: %q", got)
	}
	if got := env("NOTE"); got != "second" {
		t.Fatalf("a held change was applied: NOTE=%q", got)
	}
	if got := dockerExec(t, web(), "cat /data/file"); got != "precious" {
		t.Fatalf("a held push touched the volume: %q", got)
	}

	// Confirming it applies the change and clears the hold.
	resp, err = c.Post(base+"/update", "application/json", strings.NewReader(fmt.Sprintf(`{"name":%q,"allowDataLoss":true}`, stack)))
	if err != nil {
		t.Fatal(err)
	}
	msg, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("update: %d %s", resp.StatusCode, msg)
	}
	eventually(t, 2*time.Minute, func() bool { return env("NOTE") == "third" })
	if got := held(); got != "" {
		t.Fatalf("the hold stayed: %q", got)
	}
}

// TestComposeStackPostgres18 runs the Postgres image that moved its data folder, and checks data survives a restart.
func TestComposeStackPostgres18(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	bin := filepath.Join(root, "skiff")
	run(t, "", nil, "go", "build", "-o", bin, "..")
	protectExisting(t, home)
	panel := freeAddr(t)
	start(t, []string{"HOME=" + home, "SKIFF_PANEL_PASSWORD=e2e-setup"}, bin, "panel", "--addr", panel, "--domain", "localhost")
	waitHTTP(t, "http://"+panel+"/api/me")
	c := client(t)
	post(t, c, "http://"+panel+"/api/auth/setup", `{"secret":"e2e-setup","email":"e2e@example.com","name":"E2E","password":"e2e-password-1"}`)

	base := "http://" + panel + "/api/stacks"
	const stack = "e2e-pg18"
	t.Cleanup(func() {
		req, _ := http.NewRequest(http.MethodDelete, base+"?name="+stack, nil)
		if resp, err := c.Do(req); err == nil {
			resp.Body.Close()
		}
	})
	const file = `services:\n  db:\n    image: postgres:18-alpine\n    environment: {POSTGRES_USER: app, POSTGRES_PASSWORD: pw, POSTGRES_DB: shop}\n`
	resp, err := c.Post(base, "application/json", strings.NewReader(`{"name":"`+stack+`","compose":"`+file+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("creating the stack: %d %s", resp.StatusCode, msg)
	}
	container := func() string {
		out, _ := exec.Command("docker", "ps", "-q", "--filter", "label=skiff.stack="+stack, "--filter", "label=skiff.kind=database", "--filter", "status=running").Output()
		return strings.TrimSpace(string(out))
	}
	ready := func() bool {
		id := container()
		if id == "" {
			return false
		}
		// Over TCP, because the server Postgres runs during first-time setup only listens on its socket.
		return exec.Command("docker", "exec", id, "pg_isready", "-h", "127.0.0.1", "-U", "app", "-d", "shop").Run() == nil
	}
	eventually(t, 3*time.Minute, ready)
	dockerExec(t, container(), "psql -U app -d shop -c 'create table kept (id int)'")
	if out, err := exec.Command("docker", "restart", container()).CombinedOutput(); err != nil {
		t.Fatalf("restart: %v %s", err, out)
	}
	eventually(t, time.Minute, ready)
	if got := dockerExec(t, container(), "psql -U app -d shop -tAc \"select to_regclass('kept')\""); got != "kept" {
		t.Fatalf("the data didn't survive a restart: %q", got)
	}
}

// TestComposeStackMounts deploys a stack that mounts files from its own repository, and checks each kind of mount does what its file asked.
func TestComposeStackMounts(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	bin := filepath.Join(root, "skiff")
	run(t, "", nil, "go", "build", "-o", bin, "..")
	repo, push := gitServer(t, root)
	work := filepath.Join(root, "work")
	for path, content := range map[string]string{
		"deploy/compose.yml": `services:
  web:
    image: busybox:1.36
    command: httpd -f -p 3000 -h /www
    ports: ["3000"]
    volumes:
      - ./conf/app.conf:/etc/app.conf:ro
      - ./site:/www:ro
      - ./seed:/seed
      - ./data:/data
      - ..:/whole-repo:ro
  built:
    build: ../api
    command: sleep 100000
    volumes: ["./conf/app.conf:/etc/app.conf"]
`,
		"deploy/conf/app.conf":   "from-the-repo\n",
		"deploy/site/index.html": "<h1>mounted site</h1>\n",
		"deploy/seed/start.txt":  "seeded\n",
		"api/Dockerfile":         "FROM busybox:1.36\nRUN echo built > /marker\n",
	} {
		full := filepath.Join(work, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	push("v1")

	protectExisting(t, home)
	panel := freeAddr(t)
	start(t, []string{"HOME=" + home, "SKIFF_PANEL_PASSWORD=e2e-setup", "SKIFF_ALLOW_PRIVATE_ENDPOINTS=1"}, bin, "panel", "--addr", panel, "--domain", "localhost")
	waitHTTP(t, "http://"+panel+"/api/me")
	c := client(t)
	post(t, c, "http://"+panel+"/api/auth/setup", `{"secret":"e2e-setup","email":"e2e@example.com","name":"E2E","password":"e2e-password-1"}`)

	base := "http://" + panel + "/api/stacks"
	const stack = "e2e-mounts"
	t.Cleanup(func() {
		req, _ := http.NewRequest(http.MethodDelete, base+"?name="+stack, nil)
		if resp, err := c.Do(req); err == nil {
			resp.Body.Close()
		}
	})
	body := fmt.Sprintf(`{"name":%q,"git":%q,"path":"deploy/compose.yml"}`, stack, repo)
	resp, err := c.Post(base, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("creating the stack: %d %s", resp.StatusCode, msg)
	}
	running := func(app string) string {
		out, _ := exec.Command("docker", "ps", "-q", "--filter", "label=skiff.app="+app, "--filter", "status=running").Output()
		if f := strings.Fields(string(out)); len(f) > 0 {
			return f[0]
		}
		return ""
	}
	eventually(t, 5*time.Minute, func() bool { return running(stack+"-web") != "" && running(stack+"-built") != "" })

	web := running(stack + "-web")
	if got := dockerExec(t, web, "cat /etc/app.conf"); got != "from-the-repo" {
		t.Errorf("a repository file: %q", got)
	}
	if got := dockerExec(t, web, "wget -qO- http://127.0.0.1:3000/"); !strings.Contains(got, "mounted site") {
		t.Errorf("a read-only folder: %q", got)
	}
	if got := dockerExec(t, web, "cat /seed/start.txt"); got != "seeded" {
		t.Errorf("a writable folder should start with the repository's copy: %q", got)
	}
	if got := dockerExec(t, web, "ls -A /data | wc -l"); got != "0" {
		t.Errorf("a folder the repository doesn't have should start empty: %q", got)
	}
	if got := dockerExec(t, web, "ls -a /whole-repo"); strings.Contains(got, ".git") || !strings.Contains(got, "api") {
		t.Errorf("the whole repository, without .git: %q", got)
	}
	built := running(stack + "-built")
	if got := dockerExec(t, built, "cat /marker /etc/app.conf"); got != "built\nfrom-the-repo" {
		t.Errorf("a file on top of the service's own Dockerfile: %q", got)
	}

	// Data written to a volume outlives the deploy that carries the repository's next version.
	dockerExec(t, web, "echo mine > /data/note; echo changed > /seed/start.txt")
	if err := os.WriteFile(filepath.Join(work, "deploy/conf/app.conf"), []byte("second-version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	push("v2")
	redeploy, err := c.Post("http://"+panel+"/api/redeploy?app="+stack+"-web", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	redeploy.Body.Close()
	eventually(t, 3*time.Minute, func() bool {
		id := running(stack + "-web")
		if id == "" {
			return false
		}
		out, err := exec.Command("docker", "exec", id, "cat", "/etc/app.conf").Output()
		return err == nil && strings.TrimSpace(string(out)) == "second-version"
	})
	web = running(stack + "-web")
	if got := dockerExec(t, web, "cat /data/note /seed/start.txt"); got != "mine\nchanged" {
		t.Errorf("volumes should keep their changes across a deploy: %q", got)
	}
}

// TestComposeStackRunSettings checks a container starts the way its Compose file asks.
func TestComposeStackRunSettings(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	bin := filepath.Join(root, "skiff")
	run(t, "", nil, "go", "build", "-o", bin, "..")
	protectExisting(t, home)
	panel := freeAddr(t)
	start(t, []string{"HOME=" + home, "SKIFF_PANEL_PASSWORD=e2e-setup"}, bin, "panel", "--addr", panel, "--domain", "localhost")
	waitHTTP(t, "http://"+panel+"/api/me")
	c := client(t)
	post(t, c, "http://"+panel+"/api/auth/setup", `{"secret":"e2e-setup","email":"e2e@example.com","name":"E2E","password":"e2e-password-1"}`)

	base := "http://" + panel + "/api/stacks"
	const stack = "e2e-run"
	t.Cleanup(func() {
		req, _ := http.NewRequest(http.MethodDelete, base+"?name="+stack, nil)
		if resp, err := c.Do(req); err == nil {
			resp.Body.Close()
		}
	})
	const file = `services:
  app:
    image: busybox:1.36
    entrypoint: ["sh", "-c", "echo started-by-entrypoint > /scratch/mark; exec sleep 100000"]
    user: "1000:1000"
    working_dir: /scratch
    init: true
    read_only: true
    shm_size: 32m
    tmpfs: ["/scratch:mode=1777"]
    ulimits: {nofile: 4096}
    extra_hosts: ["db.local:10.9.8.7"]
    stop_grace_period: 7s
  skipped:
    image: busybox:1.36
    profiles: [debug]
`
	quoted := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(file)
	resp, err := c.Post(base, "application/json", strings.NewReader(`{"name":"`+stack+`","compose":"`+quoted+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("creating the stack: %d %s", resp.StatusCode, msg)
	}
	container := func() string {
		out, _ := exec.Command("docker", "ps", "-q", "--filter", "label=skiff.app="+stack+"-app", "--filter", "status=running").Output()
		return strings.TrimSpace(string(out))
	}
	eventually(t, 3*time.Minute, func() bool { return container() != "" })
	id := container()
	eventually(t, 30*time.Second, func() bool {
		return exec.Command("docker", "exec", container(), "test", "-f", "/scratch/mark").Run() == nil
	})
	id = container()
	for script, want := range map[string]string{
		"cat /scratch/mark":                "started-by-entrypoint",
		"id -u":                            "1000",
		"pwd":                              "/scratch",
		"ulimit -n":                        "4096",
		"grep db.local /etc/hosts | wc -l": "1",
		"df -m /dev/shm | tail -1 | awk '{print $2}'":           "32",
		"touch /nope 2>/dev/null && echo wrote || echo refused": "refused",
	} {
		if got := dockerExec(t, id, script); got != want {
			t.Errorf("%s = %q, want %q", script, got, want)
		}
	}
	out, err := exec.Command("docker", "inspect", "-f", "{{.HostConfig.Init}} {{.Config.StopTimeout}}", id).Output()
	if err != nil || strings.TrimSpace(string(out)) != "true 7" {
		t.Errorf("init and stop timeout: %q %v", out, err)
	}
	if out, _ := exec.Command("docker", "ps", "-aq", "--filter", "label=skiff.app="+stack+"-skipped").Output(); strings.TrimSpace(string(out)) != "" {
		t.Errorf("a service behind a profile started: %s", out)
	}
}

const sharedVolumeFile = `services:
  web:
    image: busybox:1.36
    command: sleep 100000
    volumes: ["uploads:/files"]
  reader:
    image: busybox:1.36
    command: sleep 100001
    volumes: ["uploads:/incoming"]
volumes: {uploads: {}}
`

// TestComposeStackSharedVolume shares a volume between two services and checks it stays in one place through updates, backups and removal.
func TestComposeStackSharedVolume(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	bin := filepath.Join(root, "skiff")
	run(t, "", nil, "go", "build", "-o", bin, "..")
	protectExisting(t, home)
	panel := freeAddr(t)
	start(t, []string{"HOME=" + home, "SKIFF_PANEL_PASSWORD=e2e-setup"}, bin, "panel", "--addr", panel, "--domain", "localhost")
	waitHTTP(t, "http://"+panel+"/api/me")
	c := client(t)
	post(t, c, "http://"+panel+"/api/auth/setup", `{"secret":"e2e-setup","email":"e2e@example.com","name":"E2E","password":"e2e-password-1"}`)

	base := "http://" + panel + "/api/stacks"
	const stack = "e2e-sharedvol"
	removed := false
	t.Cleanup(func() {
		if removed {
			return
		}
		req, _ := http.NewRequest(http.MethodDelete, base+"?name="+stack, nil)
		if resp, err := c.Do(req); err == nil {
			resp.Body.Close()
		}
	})
	body := func(file string) string {
		quoted := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(file)
		return `{"name":"` + stack + `","compose":"` + quoted + `"}`
	}
	send := func(method, url, b string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest(method, url, strings.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		msg, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(msg)
	}
	if code, msg := send(http.MethodPost, base, body(sharedVolumeFile)); code != http.StatusCreated {
		t.Fatalf("create: %d %s", code, msg)
	}
	running := func(service string) string {
		out, _ := exec.Command("docker", "ps", "-q", "--filter", "label=skiff.app="+stack+"-"+service, "--filter", "status=running").Output()
		if f := strings.Fields(string(out)); len(f) > 0 {
			return f[0]
		}
		return ""
	}
	eventually(t, 3*time.Minute, func() bool { return running("web") != "" && running("reader") != "" })
	dockerExec(t, running("web"), "echo shared > /files/note")
	if got := dockerExec(t, running("reader"), "cat /incoming/note"); got != "shared" {
		t.Fatalf("the other service should see the file: %q", got)
	}
	volumes := func() string {
		out, _ := exec.Command("docker", "volume", "ls", "-q", "--filter", "name=skiff-"+stack).Output()
		return strings.Join(strings.Fields(string(out)), " ")
	}
	if got := volumes(); got != "skiff-"+stack+"-web_uploads" {
		t.Fatalf("there should be one volume, held by the first service: %q", got)
	}

	// Writing the services in the other order changes nothing.
	reordered := strings.Replace(sharedVolumeFile, "  web:\n    image: busybox:1.36\n    command: sleep 100000\n    volumes: [\"uploads:/files\"]\n", "", 1)
	reordered = strings.Replace(reordered, "volumes: {uploads: {}}", "  web:\n    image: busybox:1.36\n    command: sleep 100000\n    volumes: [\"uploads:/files\"]\nvolumes: {uploads: {}}", 1)
	code, msg := send(http.MethodPost, base+"/changes", body(reordered))
	if code != http.StatusOK || !strings.Contains(msg, `"changes":[]`) && !strings.Contains(msg, `"changes":null`) {
		t.Fatalf("reordering should be no change: %d %s", code, msg)
	}

	// The service holding the volume can't leave while another still uses it.
	withoutHolder := "services:\n  reader:\n    image: busybox:1.36\n    command: sleep 100001\n    volumes: [\"uploads:/incoming\"]\nvolumes: {uploads: {}}\n"
	code, msg = send(http.MethodPost, base+"/changes", body(withoutHolder))
	if code != http.StatusOK || !strings.Contains(msg, "held by web") {
		t.Fatalf("removing the holder should be refused: %d %s", code, msg)
	}

	// A backup asked for through either service is the same volume's.
	backup := func(service string) {
		t.Helper()
		if code, msg := send(http.MethodPost, "http://"+panel+"/api/volumes/backups?app="+stack+"-"+service+"&volume=uploads", ""); code != http.StatusOK {
			t.Fatalf("backup through %s: %d %s", service, code, msg)
		}
	}
	backup("reader")
	var list []struct{ ID string }
	getJSON(t, c, "http://"+panel+"/api/volumes/backups?app="+stack+"-web&volume=uploads", &list)
	if len(list) != 1 {
		t.Fatalf("the holder should list the backup: %+v", list)
	}

	if code, msg := send(http.MethodDelete, base+"?name="+stack, ""); code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", code, msg)
	}
	removed = true
	eventually(t, time.Minute, func() bool { return volumes() == "" })
}
