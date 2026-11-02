//go:build e2e

// Package e2e drives the real binary through a git deploy: clone, build, release, route, redeploy, rollback, teardown.
package e2e

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cgi"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const app = "e2e-site"

func TestGitDeployLifecycle(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	bin := filepath.Join(root, "skiff")
	run(t, "", nil, "go", "build", "-o", bin, "..")
	t.Cleanup(func() {
		_ = exec.Command("sh", "-c", "docker rm -f $(docker ps -aq --filter label=skiff.app="+app+") ; docker rmi $(docker images -q skiff-"+app+")").Run()
	})

	repo, push := gitServer(t, root)
	push("v1")

	protectExisting(t, home)
	env := []string{"HOME=" + home, "SKIFF_PANEL_PASSWORD=e2e-setup"}
	panelAddr, routerAddr := freeAddr(t), freeAddr(t)
	start(t, env, bin, "panel", "--addr", panelAddr, "--domain", "localhost")
	waitHTTP(t, "http://"+panelAddr+"/api/me")
	start(t, env, bin, "router", "--domain", "localhost", "--http-addr", routerAddr, "--panel", panelAddr)

	c := client(t)
	post(t, c, "http://"+panelAddr+"/api/auth/setup", `{"secret":"e2e-setup","email":"e2e@example.com","name":"E2E","password":"e2e-password-1"}`)

	stream(t, c, "http://"+panelAddr+"/api/deploy?name="+app+"&port=8080&git="+repo)
	expectServed(t, routerAddr, "v1")

	push("v2")
	stream(t, c, "http://"+panelAddr+"/api/redeploy?app="+app)
	expectServed(t, routerAddr, "v2")

	var project struct {
		Deploys []struct {
			ID           string `json:"id"`
			Rollbackable bool   `json:"rollbackable"`
		} `json:"deploys"`
	}
	getJSON(t, c, "http://"+panelAddr+"/api/project?app="+app, &project)
	if len(project.Deploys) != 2 || !project.Deploys[1].Rollbackable {
		t.Fatalf("expected the first deploy to be rollbackable: %+v", project.Deploys)
	}
	stream(t, c, "http://"+panelAddr+"/api/rollback?app="+app+"&id="+project.Deploys[1].ID)
	expectServed(t, routerAddr, "v1")

	checkMCP(t, c, bin, panelAddr, routerAddr, project.Deploys[0].ID)
	checkVolumes(t, c, panelAddr)
	checkHealthCheck(t, c, panelAddr, routerAddr)
	checkPreviewPassword(t, c, root, panelAddr, routerAddr)
	checkImageProject(t, c, panelAddr, routerAddr)

	post(t, c, "http://"+panelAddr+"/api/down?app="+app, "")
	if out, _ := exec.Command("docker", "ps", "-aq", "--filter", "label=skiff.app="+app).Output(); strings.TrimSpace(string(out)) != "" {
		t.Fatalf("containers left after teardown: %s", out)
	}
	if out, _ := exec.Command("docker", "volume", "ls", "-q", "--filter", "label=skiff.volume.app="+app).Output(); strings.TrimSpace(string(out)) != "" {
		t.Fatalf("volumes left after teardown: %s", out)
	}
}

// checkVolumes gives the project a volume and checks a file written to it survives a redeploy onto new containers.
func checkVolumes(t *testing.T, c *http.Client, panelAddr string) {
	t.Helper()
	t.Cleanup(func() {
		_ = exec.Command("sh", "-c", "docker volume rm -f $(docker volume ls -q --filter label=skiff.volume.app="+app+") $(docker volume ls -q --filter label=skiff.volume.app="+app+"-feat)").Run()
	})
	post(t, c, "http://"+panelAddr+"/api/volumes?app="+app, `{"name":"data","path":"/data"}`)
	stream(t, c, "http://"+panelAddr+"/api/redeploy?app="+app)

	before := appContainer(t, app)
	dockerExec(t, before, "echo kept > /data/note")
	stream(t, c, "http://"+panelAddr+"/api/redeploy?app="+app)
	after := appContainer(t, app)
	if after == before {
		t.Fatal("the redeploy didn't replace the container")
	}
	if got := dockerExec(t, after, "cat /data/note"); got != "kept" {
		t.Fatalf("file on the volume after a redeploy = %q, want kept", got)
	}

	var vols struct {
		Kind    string `json:"kind"`
		Volumes []struct {
			Name     string `json:"name"`
			Attached bool   `json:"attached"`
		} `json:"volumes"`
	}
	getJSON(t, c, "http://"+panelAddr+"/api/volumes?app="+app, &vols)
	if vols.Kind != "project" || len(vols.Volumes) != 1 || !vols.Volumes[0].Attached {
		t.Fatalf("volumes view = %+v", vols)
	}
}

// checkHealthCheck sets a health check the app fails, and expects the deploy to be refused with the running version left serving; a passing one deploys.
func checkHealthCheck(t *testing.T, c *http.Client, panelAddr, routerAddr string) {
	t.Helper()
	setHealth := func(path string, timeout int) {
		var settings map[string]any
		getJSON(t, c, "http://"+panelAddr+"/api/project?app="+app, &settings)
		settings["healthPath"], settings["healthTimeout"] = path, timeout
		body, _ := json.Marshal(settings)
		req, _ := http.NewRequest(http.MethodPut, "http://"+panelAddr+"/api/project?app="+app, strings.NewReader(string(body)))
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("saving the health check: %d", resp.StatusCode)
		}
	}
	running := appContainer(t, app)
	setHealth("/missing", 5)
	if ok, log := streamResult(t, c, "http://"+panelAddr+"/api/redeploy?app="+app); ok || !strings.Contains(log, "GET /missing answered 404") {
		t.Fatalf("a failing health check should stop the deploy:\n%s", log)
	}
	if appContainer(t, app) != running {
		t.Fatal("the running version was replaced despite the failed health check")
	}
	expectServed(t, routerAddr, "v2")

	setHealth("/index.html", 0)
	stream(t, c, "http://"+panelAddr+"/api/redeploy?app="+app)
	expectServed(t, routerAddr, "v2")
}

// checkImageProject deploys a prebuilt image with its port read from the image, then moves it to a new tag through the API the way CI would.
func checkImageProject(t *testing.T, c *http.Client, panelAddr, routerAddr string) {
	t.Helper()
	const img = "e2e-img"
	t.Cleanup(func() {
		_ = exec.Command("sh", "-c", "docker rm -f $(docker ps -aq --filter label=skiff.app="+img+") ; docker rmi $(docker images -q skiff-"+img+")").Run()
	})
	var created struct{ App, ID string }
	postJSON(t, c, "http://"+panelAddr+"/api/projects/image", `{"name":"`+img+`","image":"traefik/whoami:v1.10.2"}`, &created)
	stream(t, c, "http://"+panelAddr+"/api/deploys/log?app="+img+"&id="+created.ID)
	served := func() bool {
		req, _ := http.NewRequest(http.MethodGet, "http://"+routerAddr+"/", nil)
		req.Host = img + ".localhost"
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp.StatusCode == 200 && strings.Contains(string(b), "Hostname:")
	}
	eventually(t, 30*time.Second, served)

	resp, err := c.Get("http://" + panelAddr + "/api/preview?app=" + img + "&branch=feat")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("a preview of an image project should be refused, got %d", resp.StatusCode)
	}

	var tok struct{ Token string }
	postJSON(t, c, "http://"+panelAddr+"/api/tokens", `{"name":"e2e-ci"}`, &tok)
	req, _ := http.NewRequest(http.MethodPost, "http://"+panelAddr+"/api/v1/apps/"+img+"/deploy", strings.NewReader(`{"image":"traefik/whoami:v1.10.3"}`))
	req.Header.Set("Authorization", "Bearer "+tok.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var queued struct{ ID string }
	_ = json.NewDecoder(resp.Body).Decode(&queued)
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted || queued.ID == "" {
		t.Fatalf("API deploy with a new image: %d", resp.StatusCode)
	}
	stream(t, c, "http://"+panelAddr+"/api/deploys/log?app="+img+"&id="+queued.ID)
	var project struct {
		Image   string `json:"image"`
		Deploys []struct {
			Message string `json:"message"`
		} `json:"deploys"`
	}
	getJSON(t, c, "http://"+panelAddr+"/api/project?app="+img, &project)
	if project.Image != "traefik/whoami:v1.10.3" || len(project.Deploys) == 0 || project.Deploys[0].Message != project.Image {
		t.Fatalf("after the API deploy: %+v", project)
	}
	if out, _ := exec.Command("docker", "inspect", "-f", "{{index .Config.Labels \"skiff.port\"}}", appContainer(t, img)).Output(); strings.TrimSpace(string(out)) != "80" {
		t.Fatalf("port read from the image = %q, want 80", out)
	}
	eventually(t, 30*time.Second, served)
	post(t, c, "http://"+panelAddr+"/api/down?app="+img, "")
}

func appContainer(t *testing.T, name string) string {
	t.Helper()
	out, err := exec.Command("docker", "ps", "-q", "--filter", "label=skiff.app="+name).Output()
	ids := strings.Fields(string(out))
	if err != nil || len(ids) != 1 {
		t.Fatalf("want one running container for %s, got %q (%v)", name, ids, err)
	}
	return ids[0]
}

func dockerExec(t *testing.T, container, script string) string {
	t.Helper()
	out, err := exec.Command("docker", "exec", container, "sh", "-c", script).CombinedOutput()
	if err != nil {
		t.Fatalf("docker exec %s: %v\n%s", script, err, out)
	}
	return strings.TrimSpace(string(out))
}

// checkPreviewPassword gives a project preview-only env overrides and a password, then checks the preview gets the override and the router asks for the password, while production stays public.
func checkPreviewPassword(t *testing.T, c *http.Client, root, panelAddr, routerAddr string) {
	t.Helper()
	preview := app + "-feat"
	t.Cleanup(func() {
		_ = exec.Command("sh", "-c", "docker rm -f $(docker ps -aq --filter label=skiff.app="+preview+") ; docker rmi $(docker images -q skiff-"+preview+")").Run()
	})
	run(t, filepath.Join(root, "work"), nil, "git", "push", "-q", filepath.Join(root, "srv", "site.git"), "main:feat")

	req, _ := http.NewRequest(http.MethodPut, "http://"+panelAddr+"/api/project/preview-password?app="+app, strings.NewReader(`{"password":"preview-pass-1"}`))
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("setting the preview password: %d", resp.StatusCode)
	}
	putEnv := func(scope, body string) {
		req, _ := http.NewRequest(http.MethodPut, "http://"+panelAddr+"/api/env?app="+app+scope, strings.NewReader(body))
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("saving env%s: %d", scope, resp.StatusCode)
		}
	}
	putEnv("", `{"vars":[{"key":"DATABASE_URL","value":"postgres://prod"},{"key":"REGION","value":"eu"}]}`)
	putEnv("&scope=preview", `{"vars":[{"key":"DATABASE_URL","value":"postgres://staging"}]}`)

	stream(t, c, "http://"+panelAddr+"/api/preview?app="+app+"&branch=feat")

	out, err := exec.Command("sh", "-c", "docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' $(docker ps -q --filter label=skiff.app="+preview+" | head -1)").Output()
	if err != nil {
		t.Fatalf("inspecting the preview: %v", err)
	}
	if env := string(out); !strings.Contains(env, "DATABASE_URL=postgres://staging") || !strings.Contains(env, "REGION=eu") || strings.Contains(env, "postgres://prod") {
		t.Fatalf("preview should get the override and the project's other vars:\n%s", env)
	}
	if got := dockerExec(t, appContainer(t, preview), "ls -A /data"); got != "" {
		t.Fatalf("preview should mount an empty volume of its own, found %q", got)
	}

	visit := func(host, method, path string, form url.Values, cookies []*http.Cookie) (*http.Response, string) {
		var body io.Reader
		if form != nil {
			body = strings.NewReader(form.Encode())
		}
		req, _ := http.NewRequest(method, "http://"+routerAddr+path, body)
		req.Host = host
		if form != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		for _, ck := range cookies {
			req.AddCookie(ck)
		}
		noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		resp, err := noRedirect.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp, string(b)
	}

	resp, body := visit(preview+".localhost", "GET", "/", nil, nil)
	if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(body, "Password required") {
		gates, _ := os.ReadFile(filepath.Join(root, "home", ".skiff", "protected.json"))
		t.Fatalf("preview wasn't locked: %d %.200s (protected: %s)", resp.StatusCode, body, gates)
	}
	if resp, body := visit(app+".localhost", "GET", "/", nil, nil); resp.StatusCode != 200 || !strings.Contains(body, "site ") {
		t.Fatalf("production was locked too: %d", resp.StatusCode)
	}
	if resp, _ := visit(preview+".localhost", "POST", "/__skiff/unlock", url.Values{"password": {"wrong-pass"}}, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", resp.StatusCode)
	}
	resp, _ = visit(preview+".localhost", "POST", "/__skiff/unlock", url.Values{"password": {"preview-pass-1"}, "next": {"/"}}, nil)
	if resp.StatusCode != http.StatusSeeOther || len(resp.Cookies()) == 0 {
		t.Fatalf("right password: %d", resp.StatusCode)
	}
	cookies := resp.Cookies()
	resp, body = visit(preview+".localhost", "GET", "/", nil, cookies)
	for deadline := time.Now().Add(15 * time.Second); resp.StatusCode == http.StatusNotFound && time.Now().Before(deadline); {
		time.Sleep(300 * time.Millisecond)
		resp, body = visit(preview+".localhost", "GET", "/", nil, cookies)
	}
	if resp.StatusCode != 200 || !strings.Contains(body, "site ") {
		t.Fatalf("unlocked preview wasn't served: %d %.200s", resp.StatusCode, body)
	}

	post(t, c, "http://"+panelAddr+"/api/down?app="+preview, "")
}

// checkMCP drives `skiff mcp` the way an agent would, through the token API on the live panel.
func checkMCP(t *testing.T, c *http.Client, bin, panelAddr, routerAddr, v2Deploy string) {
	t.Helper()
	resp, err := c.Post("http://"+panelAddr+"/api/tokens", "application/json", strings.NewReader(`{"name":"e2e-agent"}`))
	if err != nil {
		t.Fatal(err)
	}
	var tok struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&tok)
	resp.Body.Close()
	if tok.Token == "" {
		t.Fatal("no API token")
	}

	cmd := exec.Command(bin, "mcp")
	cmd.Env = append(os.Environ(), "SKIFF_URL=http://"+panelAddr, "SKIFF_TOKEN="+tok.Token)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stdin.Close(); _ = cmd.Wait() }()
	out := bufio.NewScanner(stdout)
	out.Buffer(make([]byte, 0, 64*1024), 8<<20)
	id := 0
	call := func(method string, params any) map[string]any {
		t.Helper()
		id++
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
		_, _ = stdin.Write(append(b, '\n'))
		if !out.Scan() {
			t.Fatalf("mcp closed during %s", method)
		}
		var r map[string]any
		_ = json.Unmarshal(out.Bytes(), &r)
		return r
	}
	tool := func(name string, args map[string]any) (string, bool) {
		t.Helper()
		r, _ := call("tools/call", map[string]any{"name": name, "arguments": args})["result"].(map[string]any)
		if r == nil {
			t.Fatalf("%s: no result", name)
		}
		text := r["content"].([]any)[0].(map[string]any)["text"].(string)
		return text, r["isError"].(bool)
	}

	call("initialize", map[string]any{"protocolVersion": "2025-06-18"})
	tools := call("tools/list", nil)["result"].(map[string]any)["tools"].([]any)
	if len(tools) < 8 {
		t.Fatalf("only %d tools", len(tools))
	}
	if text, isErr := tool("list_projects", nil); isErr || !strings.Contains(text, app) {
		t.Fatalf("list_projects: %s", text)
	}
	if text, isErr := tool("list_deploys", map[string]any{"name": app}); isErr || !strings.Contains(text, "rollbackable") {
		t.Fatalf("list_deploys: %s", text)
	}
	if text, isErr := tool("app_logs", map[string]any{"name": app, "lines": 20}); isErr {
		t.Fatalf("app_logs: %s", text)
	}
	if text, isErr := tool("app_traffic", map[string]any{"name": app, "range": "1h"}); isErr || !strings.Contains(text, "uptime") {
		t.Fatalf("app_traffic: %s", text)
	}
	if text, isErr := tool("set_env", map[string]any{"name": app, "set": map[string]any{"GREETING": "hi"}}); isErr {
		t.Fatalf("set_env: %s", text)
	}
	tool("set_env", map[string]any{"name": app, "set": map[string]any{"OTHER": "x"}})
	if text, _ := tool("get_env", map[string]any{"name": app}); !strings.Contains(text, "GREETING") || !strings.Contains(text, "OTHER") {
		t.Fatalf("set_env should merge, not replace: %s", text)
	}
	if text, isErr := tool("rollback", map[string]any{"name": app, "deploy": v2Deploy}); isErr || !strings.Contains(text, "live") {
		t.Fatalf("rollback via mcp: %s", text)
	}
	expectServed(t, routerAddr, "v2")
	if text, isErr := tool("get_project", map[string]any{"name": "not-mine"}); !isErr || !strings.Contains(text, "not found") {
		t.Fatalf("unknown project should be an error result: %s", text)
	}
}

// protectExisting lists the machine's existing skiff containers in the test registry, so the panel's startup reaper treats them as known instead of deleting them.
func protectExisting(t *testing.T, home string) {
	t.Helper()
	out, err := exec.Command("docker", "ps", "-a", "--filter", "label=skiff=1", "--format", `{{.Names}}|{{.Label "skiff.app"}}`).Output()
	if err != nil {
		t.Fatal(err)
	}
	type replica struct {
		Container string `json:"container"`
	}
	type entry struct {
		Name      string    `json:"name"`
		Container string    `json:"container"`
		Replicas  []replica `json:"replicas"`
	}
	apps := map[string]*entry{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name, label, ok := strings.Cut(line, "|")
		if !ok || name == "" {
			continue
		}
		if label == "" {
			label = name
		}
		e := apps[label]
		if e == nil {
			e = &entry{Name: label, Container: name}
			apps[label] = e
		}
		e.Replicas = append(e.Replicas, replica{Container: name})
	}
	b, _ := json.Marshal(apps)
	dir := filepath.Join(home, ".skiff")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "apps.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// gitServer serves a bare repo over git's smart HTTP protocol and returns a push func that commits a version of the site.
func gitServer(t *testing.T, root string) (string, func(version string)) {
	work := filepath.Join(root, "work")
	bare := filepath.Join(root, "srv", "site.git")
	run(t, "", nil, "git", "init", "-q", "-b", "main", work)
	run(t, "", nil, "git", "init", "-q", "--bare", "-b", "main", bare)
	gitEnv := []string{"GIT_AUTHOR_NAME=e2e", "GIT_AUTHOR_EMAIL=e2e@example.com", "GIT_COMMITTER_NAME=e2e", "GIT_COMMITTER_EMAIL=e2e@example.com"}
	backend, err := exec.Command("git", "--exec-path").Output()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(&cgi.Handler{
		Path: filepath.Join(strings.TrimSpace(string(backend)), "git-http-backend"),
		Env:  []string{"GIT_PROJECT_ROOT=" + filepath.Join(root, "srv"), "GIT_HTTP_EXPORT_ALL=1"},
	})
	t.Cleanup(srv.Close)
	push := func(version string) {
		page := "<!doctype html><title>e2e</title><h1>site " + version + "</h1>\n"
		if err := os.WriteFile(filepath.Join(work, "index.html"), []byte(page), 0o644); err != nil {
			t.Fatal(err)
		}
		run(t, work, gitEnv, "git", "add", "-A")
		run(t, work, gitEnv, "git", "commit", "-q", "-m", "site "+version)
		run(t, work, gitEnv, "git", "push", "-q", bare, "main")
	}
	return srv.URL + "/site.git", push
}

func expectServed(t *testing.T, routerAddr, version string) {
	t.Helper()
	want := "site " + version
	deadline := time.Now().Add(30 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, "http://"+routerAddr+"/", nil)
		req.Host = app + ".localhost"
		if resp, err := http.DefaultClient.Do(req); err == nil {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			last = fmt.Sprintf("%d %s", resp.StatusCode, b)
			if resp.StatusCode == 200 && strings.Contains(string(b), want) {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("router never served %q; last response: %s", want, last)
}

// stream follows a deploy's SSE log to the end and fails the test unless it finished ok.
func stream(t *testing.T, c *http.Client, url string) {
	t.Helper()
	if ok, log := streamResult(t, c, url); !ok {
		t.Fatalf("deploy failed:\n%s", log)
	}
}

// streamResult follows a deploy's SSE log to the end and reports whether it finished ok, with the log.
func streamResult(t *testing.T, c *http.Client, url string) (bool, string) {
	t.Helper()
	resp, err := c.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("%s: %d %s", url, resp.StatusCode, b)
	}
	var log []string
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := strings.TrimPrefix(sc.Text(), "data: ")
		if line == "" {
			continue
		}
		log = append(log, line)
		if strings.HasPrefix(line, "[done]") {
			return line == "[done] ok", strings.Join(log, "\n")
		}
	}
	t.Fatalf("log ended without a result:\n%s", strings.Join(log, "\n"))
	return false, ""
}

func client(t *testing.T) *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, Timeout: 15 * time.Minute}
}

func post(t *testing.T, c *http.Client, url, body string) {
	t.Helper()
	resp, err := c.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		t.Fatalf("POST %s: %d", url, resp.StatusCode)
	}
}

func getJSON(t *testing.T, c *http.Client, url string, v any) {
	t.Helper()
	resp, err := c.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
}

func run(t *testing.T, dir string, env []string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

func start(t *testing.T, env []string, bin string, args ...string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), env...)
	logf, _ := os.Create(filepath.Join(t.TempDir(), args[0]+".log"))
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		if t.Failed() {
			b, _ := os.ReadFile(logf.Name())
			t.Logf("%s log:\n%s", args[0], b)
		}
	})
}

func freeAddr(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

func waitHTTP(t *testing.T, url string) {
	t.Helper()
	for range 100 {
		if resp, err := http.Get(url); err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("%s never came up", url)
}
