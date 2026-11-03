//go:build e2e

package e2e

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestTCPService serves a real non-HTTP service through the router, and keeps its address across a redeploy.
func TestTCPService(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	bin := filepath.Join(root, "skiff")
	run(t, "", nil, "go", "build", "-o", bin, "..")
	protectExisting(t, home)
	env := []string{"HOME=" + home, "SKIFF_PANEL_PASSWORD=e2e-setup"}
	panel, routerAddr := freeAddr(t), freeAddr(t)
	start(t, env, bin, "panel", "--addr", panel, "--domain", "localhost")
	waitHTTP(t, "http://"+panel+"/api/me")
	start(t, env, bin, "router", "--domain", "localhost", "--http-addr", routerAddr, "--panel", panel)
	c := client(t)
	post(t, c, "http://"+panel+"/api/auth/setup", `{"secret":"e2e-setup","email":"e2e@example.com","name":"E2E","password":"e2e-password-1"}`)

	const app = "e2e-tcp"
	t.Cleanup(func() {
		_ = exec.Command("sh", "-c", "docker rm -f $(docker ps -aq --filter label=skiff.app="+app+") ; docker rmi $(docker images -q skiff-"+app+")").Run()
	})
	var created struct{ App, ID string }
	postJSON(t, c, "http://"+panel+"/api/projects/image", `{"name":"`+app+`","image":"redis:7-alpine","port":"6379"}`, &created)

	addr := freeAddr(t)
	port := addr[strings.LastIndex(addr, ":")+1:]
	put(t, c, "http://"+panel+"/api/project/tcp-port?app="+app, `{"port":`+port+`}`)
	// Redis doesn't speak HTTP, so the deploy that started with the project may fail. A redeploy uses the port-open check.
	eventually(t, 2*time.Minute, func() bool {
		var project struct {
			Deploys []struct{ Status string } `json:"deploys"`
		}
		getJSON(t, c, "http://"+panel+"/api/project?app="+app, &project)
		return len(project.Deploys) > 0 && project.Deploys[0].Status != "building"
	})
	stream(t, c, "http://"+panel+"/api/redeploy?app="+app)

	ping := func() error {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 2*time.Second)
		if err != nil {
			return err
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		if _, err := conn.Write([]byte("PING\r\n")); err != nil {
			return err
		}
		line, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			return err
		}
		if !strings.HasPrefix(line, "+PONG") {
			return fmt.Errorf("redis answered %q", line)
		}
		return nil
	}
	eventually(t, 30*time.Second, func() bool { return ping() == nil })

	// A new deploy answers on the same public port.
	before, _ := exec.Command("docker", "ps", "-q", "--filter", "label=skiff.app="+app).Output()
	stream(t, c, "http://"+panel+"/api/redeploy?app="+app)
	after, _ := exec.Command("docker", "ps", "-q", "--filter", "label=skiff.app="+app).Output()
	if strings.TrimSpace(string(before)) == strings.TrimSpace(string(after)) {
		t.Fatal("the redeploy didn't replace the container")
	}
	eventually(t, 15*time.Second, func() bool { return ping() == nil })

	// Taken and reserved ports are refused with a reason.
	for body, want := range map[string]string{`{"port":80}`: "1024", `{"port":7070}`: "dashboard"} {
		req, _ := http.NewRequest(http.MethodPut, "http://"+panel+"/api/project/tcp-port?app="+app, strings.NewReader(body))
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		msg, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(msg), want) {
			t.Errorf("%s: %d %s", body, resp.StatusCode, msg)
		}
	}
	held, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	req, _ := http.NewRequest(http.MethodPut, "http://"+panel+"/api/project/tcp-port?app="+app, strings.NewReader(fmt.Sprintf(`{"port":%d}`, held.Addr().(*net.TCPAddr).Port)))
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(msg), "in use") {
		t.Errorf("a port another program holds: %d %s", resp.StatusCode, msg)
	}

	// Closing it takes effect without a router restart.
	put(t, c, "http://"+panel+"/api/project/tcp-port?app="+app, `{"port":0}`)
	eventually(t, 15*time.Second, func() bool {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, time.Second)
		if err != nil {
			return true
		}
		conn.Close()
		return false
	})
}
