//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDisasterRecovery loses a server — its state, a database container and its volume — and brings everything back on a fresh one from offsite copies.
func TestDisasterRecovery(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	root := t.TempDir()
	oldHome, newHome := filepath.Join(root, "old"), filepath.Join(root, "new")
	bin := filepath.Join(root, "skiff")
	run(t, "", nil, "go", "build", "-o", bin, "..")

	s3Port := freeAddr(t)[len("127.0.0.1:"):]
	run(t, "", nil, "docker", "run", "-d", "--name", "skiff-e2e-s3", "-p", "127.0.0.1:"+s3Port+":9000",
		"-e", "MINIO_ROOT_USER=e2euser", "-e", "MINIO_ROOT_PASSWORD=e2epass12345", "minio/minio", "server", "/data")
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", "skiff-e2e-s3").Run() })
	waitHTTP(t, "http://127.0.0.1:"+s3Port+"/minio/health/live")
	run(t, "", nil, "docker", "exec", "skiff-e2e-s3", "mkdir", "-p", "/data/offsite")

	protectExisting(t, oldHome)
	env := []string{"HOME=" + oldHome, "SKIFF_PANEL_PASSWORD=e2e-setup", "SKIFF_ALLOW_PRIVATE_ENDPOINTS=1"}
	panel := freeAddr(t)
	stopOld := startStoppable(t, env, bin, "panel", "--addr", panel, "--domain", "localhost")
	waitHTTP(t, "http://"+panel+"/api/me")
	c := client(t)
	post(t, c, "http://"+panel+"/api/auth/setup", `{"secret":"e2e-setup","email":"e2e@example.com","name":"E2E","password":"e2e-password-1"}`)

	var db struct{ ID string }
	postJSON(t, c, "http://"+panel+"/api/databases", `{"engine":"postgres","name":"orders"}`, &db)
	ctr := "skiff-db-" + db.ID
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", ctr).Run()
		_ = exec.Command("docker", "volume", "rm", ctr+"-data").Run()
	})
	eventually(t, 90*time.Second, func() bool {
		return exec.Command("docker", "exec", ctr, "pg_isready", "-U", "skiff", "-d", "orders").Run() == nil
	})
	time.Sleep(2 * time.Second)
	run(t, "", nil, "docker", "exec", ctr, "psql", "-U", "skiff", "-d", "orders", "-qc",
		"create table orders(item text); insert into orders values ('survived');")

	put(t, c, "http://"+panel+"/api/backups/offsite", fmt.Sprintf(
		`{"endpoint":"http://127.0.0.1:%s","bucket":"offsite","prefix":"skiff","accessKey":"e2euser","secretKey":"e2epass12345"}`, s3Port))
	post(t, c, "http://"+panel+"/api/backups?db="+db.ID, "")
	eventually(t, 30*time.Second, func() bool {
		var backups []struct{ Offsite string }
		getJSON(t, c, "http://"+panel+"/api/backups?db="+db.ID, &backups)
		return len(backups) > 0 && backups[0].Offsite == "ok"
	})

	var cp struct{ RecoveryKey string }
	getJSON(t, c, "http://"+panel+"/api/control-plane?reveal=1", &cp)
	resp, err := c.Get("http://" + panel + "/api/control-plane?download=1")
	if err != nil {
		t.Fatal(err)
	}
	bundle, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.Contains(string(bundle), "e2epass12345") {
		t.Fatal("the bundle leaks a secret in plain text")
	}
	bundlePath := filepath.Join(root, "cp.skiffbak")
	_ = os.WriteFile(bundlePath, bundle, 0o600)

	// The server is lost: panel, database container and its volume.
	stopOld()
	run(t, "", nil, "docker", "rm", "-f", ctr)
	run(t, "", nil, "docker", "volume", "rm", ctr+"-data")

	if out, err := exec.Command(bin, "server", "restore", bundlePath, "--key", strings.Repeat("0", 64)).CombinedOutput(); err == nil {
		t.Fatalf("restored with the wrong key: %s", out)
	}
	cmd := exec.Command(bin, "server", "restore", bundlePath, "--key", cp.RecoveryKey)
	cmd.Env = append(os.Environ(), "HOME="+newHome)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("restore: %v\n%s", err, out)
	}
	keepExisting(t, oldHome, newHome)

	newPanel := freeAddr(t)
	start(t, []string{"HOME=" + newHome, "SKIFF_PANEL_PASSWORD=e2e-setup", "SKIFF_ALLOW_PRIVATE_ENDPOINTS=1"}, bin, "panel", "--addr", newPanel, "--domain", "localhost")
	eventually(t, 3*time.Minute, func() bool {
		out, _ := exec.Command("docker", "exec", ctr, "psql", "-U", "skiff", "-d", "orders", "-tAc", "select item from orders").Output()
		return strings.TrimSpace(string(out)) == "survived"
	})

	c2 := client(t)
	post(t, c2, "http://"+newPanel+"/api/auth/login", `{"email":"e2e@example.com","password":"e2e-password-1"}`)
	var dbs []struct{ Name string }
	getJSON(t, c2, "http://"+newPanel+"/api/databases", &dbs)
	if len(dbs) != 1 || dbs[0].Name != "orders" {
		t.Fatalf("databases on the new server: %+v", dbs)
	}
}

// keepExisting carries the machine's protected containers into the restored registry, as protectExisting did for the old one.
func keepExisting(t *testing.T, from, to string) {
	t.Helper()
	read := func(home string) map[string]json.RawMessage {
		m := map[string]json.RawMessage{}
		b, _ := os.ReadFile(filepath.Join(home, ".skiff", "apps.json"))
		_ = json.Unmarshal(b, &m)
		return m
	}
	merged := read(to)
	for k, v := range read(from) {
		merged[k] = v
	}
	b, _ := json.Marshal(merged)
	_ = os.WriteFile(filepath.Join(to, ".skiff", "apps.json"), b, 0o600)
}

func eventually(t *testing.T, limit time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("condition not met within %s", limit)
}

func postJSON(t *testing.T, c *http.Client, url, body string, out any) {
	t.Helper()
	resp, err := c.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST %s: %d %s", url, resp.StatusCode, b)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatal(err)
	}
}

func put(t *testing.T, c *http.Client, url, body string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPut, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		t.Fatalf("PUT %s: %d %s", url, resp.StatusCode, b)
	}
}

// startStoppable starts a process and returns a function that stops it early; it's also stopped at cleanup.
func startStoppable(t *testing.T, env []string, bin string, args ...string) func() {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), env...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := false
	stop := func() {
		if !stopped {
			stopped = true
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	}
	t.Cleanup(stop)
	return stop
}
