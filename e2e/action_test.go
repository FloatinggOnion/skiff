//go:build e2e

package e2e

import (
	"bufio"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// TestGitHubAction runs the action's script against a real panel, the way a workflow would.
func TestGitHubAction(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not available")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	bin := filepath.Join(root, "skiff")
	run(t, "", nil, "go", "build", "-o", bin, "..")
	repo, push := gitServer(t, root)
	push("v1")
	work := filepath.Join(root, "work")
	bare := filepath.Join(root, "srv", "site.git")
	run(t, work, nil, "git", "branch", "feat/login")
	run(t, work, nil, "git", "push", "-q", bare, "feat/login")

	protectExisting(t, home)
	panel := freeAddr(t)
	start(t, []string{"HOME=" + home, "SKIFF_PANEL_PASSWORD=e2e-setup", "SKIFF_ALLOW_PRIVATE_ENDPOINTS=1"}, bin, "panel", "--addr", panel, "--domain", "localhost")
	waitHTTP(t, "http://"+panel+"/api/me")
	c := client(t)
	post(t, c, "http://"+panel+"/api/auth/setup", `{"secret":"e2e-setup","email":"e2e@example.com","name":"E2E","password":"e2e-password-1"}`)
	const gitApp, imgApp = "e2e-act", "e2e-act-img"
	t.Cleanup(func() {
		for _, a := range []string{gitApp, gitApp + "-feat-login", imgApp} {
			_ = exec.Command("sh", "-c", "docker rm -f $(docker ps -aq --filter label=skiff.app="+a+") ; docker rmi $(docker images -q skiff-"+a+")").Run()
		}
	})
	stream(t, c, "http://"+panel+"/api/deploy?name="+gitApp+"&port=8080&git="+repo)
	var created struct{ App, ID string }
	postJSON(t, c, "http://"+panel+"/api/projects/image", `{"name":"`+imgApp+`","image":"traefik/whoami:v1.10.2"}`, &created)
	stream(t, c, "http://"+panel+"/api/deploys/log?app="+imgApp+"&id="+created.ID)
	var tok struct{ Token string }
	postJSON(t, c, "http://"+panel+"/api/tokens", `{"name":"e2e-action"}`, &tok)

	script, err := filepath.Abs(filepath.Join("..", "scripts", "github-action.sh"))
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		ok      bool
		out     string
		outputs map[string]string
	}
	action := func(env ...string) result {
		t.Helper()
		file := filepath.Join(t.TempDir(), "output")
		cmd := exec.Command("bash", script)
		cmd.Env = append(os.Environ(), "GITHUB_OUTPUT="+file, "SKIFF_POLL=1", "SKIFF_SERVER=http://"+panel, "SKIFF_TOKEN="+tok.Token)
		cmd.Env = append(cmd.Env, env...)
		out, err := cmd.CombinedOutput()
		r := result{ok: err == nil, out: string(out), outputs: map[string]string{}}
		if f, ferr := os.Open(file); ferr == nil {
			defer f.Close()
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				k, v, _ := strings.Cut(sc.Text(), "=")
				r.outputs[k] = v
			}
		}
		if strings.Contains(r.out, tok.Token) {
			t.Fatalf("the output shows the token: %s", r.out)
		}
		return r
	}

	r := action("SKIFF_APP=" + gitApp)
	if !r.ok || r.outputs["app"] != gitApp || r.outputs["url"] == "" || r.outputs["deploy-id"] == "" || len(r.outputs["commit"]) < 7 || !strings.Contains(r.out, "is live") {
		t.Fatalf("deploy: %+v", r)
	}

	if r := action("SKIFF_APP="+gitApp, "GITHUB_SHA="+r.outputs["commit"]+"aaaa"); !r.ok || strings.Contains(r.out, "::notice::") {
		t.Fatalf("the workflow's own commit shouldn't warn: %+v", r)
	}
	if r := action("SKIFF_APP="+gitApp, "GITHUB_SHA=0000000aaaa"); !r.ok || !strings.Contains(r.out, "not 0000000") {
		t.Fatalf("a newer commit should be mentioned: %+v", r)
	}

	r = action("SKIFF_APP="+imgApp, "SKIFF_IMAGE=traefik/whoami:v1.10.1")
	if !r.ok {
		t.Fatalf("deploy of a new image: %+v", r)
	}
	img, _ := exec.Command("sh", "-c", "docker inspect -f '{{.Config.Labels}}' $(docker ps -q --filter label=skiff.app="+imgApp+")").Output()
	if len(img) == 0 {
		t.Fatal("the image project isn't running")
	}

	// A failed deploy fails the step, and shows why.
	r = action("SKIFF_APP="+imgApp, "SKIFF_IMAGE=traefik/whoami:does-not-exist")
	if r.ok || !strings.Contains(r.out, "::error::") || !strings.Contains(r.out, "ended as failed") || !strings.Contains(r.out, "Build log") {
		t.Fatalf("a failing deploy: %+v", r)
	}

	// Mistakes get clear messages.
	for name, tc := range map[string]struct {
		env  []string
		want string
	}{
		"bad token":   {[]string{"SKIFF_APP=" + gitApp, "SKIFF_TOKEN=wrong"}, "rejected the API token"},
		"unknown app": {[]string{"SKIFF_APP=nope"}, "app not found"},
		"no server":   {[]string{"SKIFF_APP=" + gitApp, "SKIFF_SERVER=http://127.0.0.1:1"}, "couldn't reach"},
		"no app":      {[]string{"SKIFF_APP="}, "Set the app input"},
		"bad command": {[]string{"SKIFF_APP=" + gitApp, "SKIFF_COMMAND=launch"}, "Unknown command"},
		"image only":  {[]string{"SKIFF_APP=" + gitApp, "SKIFF_IMAGE=a/b:1"}, "builds from a repository"},
		"no preview":  {[]string{"SKIFF_APP=" + imgApp, "SKIFF_COMMAND=preview", "SKIFF_BRANCH=x"}, "prebuilt image"},
	} {
		if r := action(tc.env...); r.ok || !strings.Contains(r.out, tc.want) {
			t.Errorf("%s: %+v", name, r)
		}
	}

	// A pipeline can create a preview of a branch, reach it, and remove it.
	r = action("SKIFF_APP="+gitApp, "SKIFF_COMMAND=preview", "GITHUB_HEAD_REF=feat/login")
	if !r.ok || r.outputs["app"] != gitApp+"-feat-login" || !strings.Contains(r.outputs["url"], gitApp+"-feat-login") {
		t.Fatalf("preview: %+v", r)
	}
	running := func(app string) string {
		out, _ := exec.Command("docker", "ps", "-q", "--filter", "label=skiff.app="+app).Output()
		return strings.TrimSpace(string(out))
	}
	if running(r.outputs["app"]) == "" {
		t.Fatal("the preview isn't running")
	}
	if r := action("SKIFF_APP="+gitApp, "SKIFF_COMMAND=preview", "SKIFF_BRANCH=feat/login"); !r.ok {
		t.Fatalf("redeploying a preview: %+v", r)
	}
	if r := action("SKIFF_APP="+gitApp, "SKIFF_COMMAND=remove-preview", "SKIFF_BRANCH=feat/login"); !r.ok || !strings.Contains(r.out, "Removed") {
		t.Fatalf("remove-preview: %+v", r)
	}
	eventually(t, time.Minute, func() bool { return running(gitApp+"-feat-login") == "" })
	if running(gitApp) == "" {
		t.Fatal("removing a preview stopped its project")
	}
	// Removing twice is fine, so a cleanup step can always run.
	if r := action("SKIFF_APP="+gitApp, "SKIFF_COMMAND=remove-preview", "SKIFF_BRANCH=feat/login"); !r.ok {
		t.Fatalf("removing a missing preview: %+v", r)
	}
	// A token only reaches its own team's previews, and a project can't remove a preview it doesn't own.
	req, _ := http.NewRequest(http.MethodDelete, "http://"+panel+"/api/v1/apps/"+imgApp+"/previews?branch=feat/login", nil)
	req.Header.Set("Authorization", "Bearer "+tok.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("removing through the wrong project: %d", resp.StatusCode)
	}
}

// TestActionMetadata checks every input the action declares reaches the script.
func TestActionMetadata(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var a struct {
		Inputs map[string]any `yaml:"inputs"`
		Runs   struct {
			Using string `yaml:"using"`
			Steps []struct {
				Env map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"runs"`
	}
	if err := yaml.Unmarshal(raw, &a); err != nil {
		t.Fatal(err)
	}
	if a.Runs.Using != "composite" || len(a.Runs.Steps) != 1 {
		t.Fatalf("%+v", a.Runs)
	}
	for in := range a.Inputs {
		key := "SKIFF_" + strings.ToUpper(in)
		if got := a.Runs.Steps[0].Env[key]; got != "${{ inputs."+in+" }}" {
			t.Errorf("%s isn't passed to the script: %q", in, got)
		}
	}
}
