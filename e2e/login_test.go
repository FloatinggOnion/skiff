//go:build e2e

package e2e

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestCLILogin signs the CLI in through the browser approval flow and uses the saved credentials.
func TestCLILogin(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	cliHome := filepath.Join(root, "laptop")
	bin := filepath.Join(root, "skiff")
	run(t, "", nil, "go", "build", "-o", bin, "..")
	protectExisting(t, home)

	panel := freeAddr(t)
	start(t, []string{"HOME=" + home, "SKIFF_PANEL_PASSWORD=e2e-setup"}, bin, "panel", "--addr", panel, "--domain", "localhost")
	waitHTTP(t, "http://"+panel+"/api/me")
	browser := client(t)
	post(t, browser, "http://"+panel+"/api/auth/setup", `{"secret":"e2e-setup","email":"e2e@example.com","name":"E2E","password":"e2e-password-1"}`)

	login := exec.Command(bin, "login", "http://"+panel)
	login.Env = append(os.Environ(), "HOME="+cliHome, "PATH=/usr/bin:/bin") // no browser to open
	out, _ := login.StdoutPipe()
	login.Stderr = login.Stdout
	if err := login.Start(); err != nil {
		t.Fatal(err)
	}
	code := ""
	sc := bufio.NewScanner(out)
	codeRe := regexp.MustCompile(`code\s+([0-9A-F]{8})`)
	for sc.Scan() && code == "" {
		if m := codeRe.FindStringSubmatch(sc.Text()); m != nil {
			code = m[1]
		}
	}
	if code == "" {
		t.Fatal("skiff login never printed a code")
	}
	go func() {
		for sc.Scan() {
		}
	}()

	var pending struct{ Code, Host string }
	getJSON(t, browser, "http://"+panel+"/api/cli/approve?code="+code, &pending)
	if pending.Code != code {
		t.Fatalf("pending = %+v", pending)
	}
	post(t, browser, "http://"+panel+"/api/cli/approve?code="+code, "")

	done := make(chan error, 1)
	go func() { done <- login.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("skiff login: %v", err)
		}
	case <-time.After(30 * time.Second):
		_ = login.Process.Kill()
		t.Fatal("skiff login didn't finish after approval")
	}

	var creds struct{ URL, Token string }
	b, err := os.ReadFile(filepath.Join(cliHome, ".skiff", "credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(b, &creds)
	if info, _ := os.Stat(filepath.Join(cliHome, ".skiff", "credentials.json")); info.Mode().Perm() != 0o600 {
		t.Fatalf("credentials are %v, want 0600", info.Mode().Perm())
	}

	whoami := exec.Command(bin, "whoami")
	whoami.Env = append(os.Environ(), "HOME="+cliHome)
	if got, err := whoami.CombinedOutput(); err != nil || !strings.Contains(string(got), panel) {
		t.Fatalf("whoami: %v %s", err, got)
	}

	// What a signed-in deploy reports: the project appears in the token's team, with the deploy.
	req, _ := http.NewRequest(http.MethodPost, "http://"+panel+"/api/v1/cli/deploys",
		strings.NewReader(`{"app":"laptop-app","id":"1793500000-abc123","commit":"0123456789abcdef","message":"ship it","port":8080}`))
	req.Header.Set("Authorization", "Bearer "+creds.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("report: %v %v", err, resp.Status)
	}
	resp.Body.Close()
	var project struct {
		Origin  string `json:"origin"`
		Port    string `json:"port"`
		Deploys []struct{ Commit, Message, Trigger string }
	}
	getJSON(t, browser, "http://"+panel+"/api/project?app=laptop-app", &project)
	if project.Origin != "cli" || project.Port != "8080" || len(project.Deploys) != 1 || project.Deploys[0].Commit != "0123456" || project.Deploys[0].Message != "ship it" {
		t.Fatalf("project = %+v", project)
	}

	// A second sign-in with the same code can't be approved again.
	resp, _ = browser.Post("http://"+panel+"/api/cli/approve?code="+code, "application/json", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("re-approving a used code: %d", resp.StatusCode)
	}
	resp.Body.Close()
}
