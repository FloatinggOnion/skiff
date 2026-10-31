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

	post(t, c, "http://"+panelAddr+"/api/down?app="+app, "")
	if out, _ := exec.Command("docker", "ps", "-aq", "--filter", "label=skiff.app="+app).Output(); strings.TrimSpace(string(out)) != "" {
		t.Fatalf("containers left after teardown: %s", out)
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
			if line != "[done] ok" {
				t.Fatalf("deploy failed:\n%s", strings.Join(log, "\n"))
			}
			return
		}
	}
	t.Fatalf("log ended without a result:\n%s", strings.Join(log, "\n"))
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
