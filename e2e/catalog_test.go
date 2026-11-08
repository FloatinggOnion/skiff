//go:build e2e

package e2e

import (
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCatalog installs every one-click app through the dashboard and checks it starts and answers, so a recipe file is only as good as what it runs. Set SKIFF_CATALOG=1 to run it; SKIFF_CATALOG_ONLY=<id> runs one.
func TestCatalog(t *testing.T) {
	if os.Getenv("SKIFF_CATALOG") != "1" {
		t.Skip("set SKIFF_CATALOG=1: this pulls and runs every one-click app")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	entries, err := os.ReadDir("../internal/panel/catalog")
	if err != nil {
		t.Fatal(err)
	}
	only := os.Getenv("SKIFF_CATALOG_ONLY")

	root := t.TempDir()
	home := filepath.Join(root, "home")
	bin := filepath.Join(root, "skiff")
	run(t, "", nil, "go", "build", "-o", bin, "..")
	protectExisting(t, home)
	env := []string{"HOME=" + home, "SKIFF_PANEL_PASSWORD=e2e-setup"}
	panel, routerAddr := freeAddr(t), freeAddr(t)
	start(t, env, bin, "panel", "--addr", panel, "--domain", "localhost")
	base := "http://" + panel
	waitHTTP(t, base+"/api/me")
	start(t, env, bin, "router", "--domain", "localhost", "--http-addr", routerAddr, "--panel", panel)
	c := client(t)
	post(t, c, base+"/api/auth/setup", `{"secret":"e2e-setup","email":"e2e@example.com","name":"E2E","password":"e2e-password-1"}`)

	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".toml")
		if !ok || only != "" && id != only {
			continue
		}
		t.Run(id, func(t *testing.T) {
			name := "cat-" + id
			t.Cleanup(func() {
				post(t, c, base+"/api/down?app="+name, "")
				var dbs []struct{ ID, Name string }
				getJSON(t, c, base+"/api/databases", &dbs)
				for _, d := range dbs {
					req, _ := http.NewRequest(http.MethodDelete, base+"/api/databases?id="+d.ID, nil)
					if resp, err := c.Do(req); err == nil {
						resp.Body.Close()
					}
				}
				_ = exec.Command("sh", "-c", "docker rm -f $(docker ps -aq --filter label=skiff.app="+name+") ; docker volume rm -f $(docker volume ls -q --filter label=skiff.volume.app="+name+")").Run()
			})
			var created struct{ App, ID string }
			postJSON(t, c, base+"/api/catalog", `{"id":"`+id+`","name":"`+name+`"}`, &created)
			if ok, log := streamResult(t, c, base+"/api/deploys/log?app="+name+"&id="+created.ID); !ok {
				t.Fatalf("the install didn't go live:\n%s", tail(log, 40))
			}
			// Some apps answer 503 while their first start runs migrations, which can outlast the health check. A redirect is an answer too: an app set up for HTTPS sends plain HTTP there, and following it would leave this test's router.
			noFollow := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			var status int
			deadline := time.Now().Add(10 * time.Minute)
			for time.Now().Before(deadline) {
				req, _ := http.NewRequest(http.MethodGet, "http://"+routerAddr+"/", nil)
				req.Host = name + ".localhost"
				if resp, err := noFollow.Do(req); err == nil {
					_, _ = io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
					if status = resp.StatusCode; status < 500 {
						return
					}
				}
				time.Sleep(5 * time.Second)
			}
			logs, _ := exec.Command("sh", "-c", "docker logs --tail 40 $(docker ps -aq --filter label=skiff.app="+name+" | head -1) 2>&1").Output()
			t.Fatalf("%s never answered without a server error, last status %d. Its last output:\n%s", id, status, logs)
		})
	}
}

func tail(s string, lines int) string {
	parts := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	return strings.Join(parts, "\n")
}
