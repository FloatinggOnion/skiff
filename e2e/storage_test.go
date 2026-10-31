//go:build e2e

package e2e

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestObjectStorage provisions a bucket through the dashboard and uses it from another container on the team network.
func TestObjectStorage(t *testing.T) {
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

	var b struct {
		ID, Name, Endpoint, AccessKey, SecretKey, State string
	}
	postJSON(t, c, "http://"+panel+"/api/storage", `{"name":"media"}`, &b)
	ctr := strings.TrimSuffix(strings.TrimPrefix(b.Endpoint, "http://"), ":8333")
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", ctr).Run()
		_ = exec.Command("docker", "volume", "rm", ctr+"-data").Run()
	})
	if b.Name != "media" || b.State != "running" {
		t.Fatalf("bucket = %+v", b)
	}
	netOut, err := exec.Command("docker", "inspect", ctr, "--format", "{{range $k, $v := .NetworkSettings.Networks}}{{$k}} {{end}}").Output()
	if err != nil {
		t.Fatal(err)
	}
	network := strings.Fields(string(netOut))[0]

	run(t, "", nil, "docker", "pull", "-q", "curlimages/curl:8.10.1")
	curl := func(user string, args ...string) string {
		t.Helper()
		full := append([]string{"run", "--rm", "--network", network, "curlimages/curl:8.10.1", "-s", "-o", "/dev/null", "-w", "%{http_code}",
			"--aws-sigv4", "aws:amz:us-east-1:s3", "--user", user}, args...)
		out, err := exec.Command("docker", full...).Output()
		if err != nil {
			t.Fatalf("curl: %v %s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	creds := b.AccessKey + ":" + b.SecretKey
	if code := curl(creds, "-X", "PUT", "--data-binary", "hello", b.Endpoint+"/media/hello.txt"); code != "200" {
		t.Fatalf("put = %s", code)
	}
	if code := curl(creds, b.Endpoint+"/media/hello.txt"); code != "200" {
		t.Fatalf("get = %s", code)
	}
	if code := curl(b.AccessKey+":wrong", b.Endpoint+"/media/hello.txt"); code != "403" {
		t.Fatalf("a wrong key got %s", code)
	}
}
