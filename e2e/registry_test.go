//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// TestPrivateRegistry deploys an image from a registry that needs a password, from the dashboard and from the CLI.
func TestPrivateRegistry(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	root := t.TempDir()

	// A registry that refuses anonymous pulls, on a port of its own.
	const user, password = "skiff", "e2e-registry-secret"
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	auth := filepath.Join(root, "auth")
	if err := os.MkdirAll(auth, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(auth, "htpasswd"), []byte(user+":"+string(hash)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, port, _ := strings.Cut(freeAddr(t), ":")
	reg := "e2e-registry-" + port
	run(t, "", nil, "docker", "run", "-d", "--name", reg, "-p", "127.0.0.1:"+port+":5000",
		"-v", auth+":/auth", "-e", "REGISTRY_AUTH=htpasswd", "-e", "REGISTRY_AUTH_HTPASSWD_REALM=e2e",
		"-e", "REGISTRY_AUTH_HTPASSWD_PATH=/auth/htpasswd", "registry:2.8.3")
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", reg).Run() })
	image := fmt.Sprintf("localhost:%s/team/whoami:1", port)

	// Push with a login kept to its own config, so this machine's Docker never holds the password.
	pushConfig := filepath.Join(root, "push-config")
	pushEnv := append(os.Environ(), "DOCKER_CONFIG="+pushConfig)
	eventually(t, 30*time.Second, func() bool {
		login := exec.Command("docker", "login", "localhost:"+port, "-u", user, "--password-stdin")
		login.Env, login.Stdin = pushEnv, strings.NewReader(password)
		return login.Run() == nil
	})
	run(t, "", nil, "docker", "pull", "-q", "traefik/whoami:v1.10.2")
	run(t, "", nil, "docker", "tag", "traefik/whoami:v1.10.2", image)
	push := exec.Command("docker", "push", "-q", image)
	push.Env = pushEnv
	if out, err := push.CombinedOutput(); err != nil {
		t.Fatalf("push: %v %s", err, out)
	}
	_ = exec.Command("docker", "rmi", image).Run()

	home := filepath.Join(root, "home")
	bin := filepath.Join(root, "skiff")
	run(t, "", nil, "go", "build", "-o", bin, "..")
	protectExisting(t, home)
	env := []string{"HOME=" + home, "SKIFF_PANEL_PASSWORD=e2e-setup"}
	panel := freeAddr(t)
	start(t, env, bin, "panel", "--addr", panel, "--domain", "localhost")
	base := "http://" + panel
	waitHTTP(t, base+"/api/me")
	c := client(t)
	post(t, c, base+"/api/auth/setup", `{"secret":"e2e-setup","email":"e2e@example.com","name":"E2E","password":"e2e-password-1"}`)

	running := func(app string) bool {
		out, _ := exec.Command("docker", "ps", "-q", "--filter", "label=skiff.app="+app).Output()
		return strings.TrimSpace(string(out)) != ""
	}

	t.Run("dashboard", func(t *testing.T) {
		const app = "e2e-private"
		t.Cleanup(func() {
			_ = exec.Command("sh", "-c", "docker rm -f $(docker ps -aq --filter label=skiff.app="+app+")").Run()
		})
		var created struct{ App, ID string }
		postJSON(t, c, base+"/api/projects/image", `{"name":"`+app+`","image":"`+image+`"}`, &created)
		if ok, log := streamResult(t, c, base+"/api/deploys/log?app="+app+"&id="+created.ID); ok {
			t.Fatalf("pulled a private image with no credentials:\n%s", log)
		}
		req, _ := http.NewRequest(http.MethodPut, base+"/api/project/registry?app="+app, strings.NewReader(`{"username":"`+user+`","password":"`+password+`"}`))
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("saving credentials: %d", resp.StatusCode)
		}
		stream(t, c, base+"/api/redeploy?app="+app)
		if !running(app) {
			t.Fatal("not running after deploying with credentials")
		}
	})

	t.Run("cli", func(t *testing.T) {
		const app = "e2e-private-cli"
		t.Cleanup(func() {
			_ = exec.Command("sh", "-c", "docker rm -f $(docker ps -aq --filter label=skiff.app="+app+")").Run()
		})
		_ = exec.Command("docker", "rmi", image).Run()
		dir := filepath.Join(root, "cli-app")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		toml := "name = \"" + app + "\"\n\n[build]\nimage = \"" + image + "\"\nport = 80\n"
		if err := os.WriteFile(filepath.Join(dir, "skiff.toml"), []byte(toml), 0o644); err != nil {
			t.Fatal(err)
		}
		run(t, dir, append(env, "SKIFF_REGISTRY_USER="+user, "SKIFF_REGISTRY_PASSWORD="+password), bin, "deploy")
		if !running(app) {
			t.Fatal("not running after skiff deploy")
		}
	})
}
