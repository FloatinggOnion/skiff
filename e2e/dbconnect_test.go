//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestDBConnect reaches private databases from real clients on this machine, through the CLI's tunnel.
func TestDBConnect(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	if runtime.GOOS != "linux" {
		t.Skip("the server reaches databases on their Docker network, which needs Linux")
	}
	root := t.TempDir()
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

	var token struct{ ID, Token string }
	postJSON(t, c, base+"/api/tokens", `{"name":"laptop","databases":true}`, &token)
	creds, _ := json.Marshal(map[string]string{"url": base, "token": token.Token})
	if err := os.WriteFile(filepath.Join(home, ".skiff", "credentials.json"), creds, 0o600); err != nil {
		t.Fatal(err)
	}

	engines := []struct {
		engine string
		image  string
		client func(rawURL string) string
	}{
		{"postgres", "postgres:16-alpine", func(u string) string { return `psql "` + u + `" -Atc "select 40+2"` }},
		{"mysql", "mysql:8", func(u string) string {
			p, _ := url.Parse(u)
			pass, _ := p.User.Password()
			return fmt.Sprintf(`MYSQL_PWD=%q mysql -h %s -P %s -u %s %s -Ne "select 40+2"`, pass, p.Hostname(), p.Port(), p.User.Username(), strings.TrimPrefix(p.Path, "/"))
		}},
		{"mongodb", "mongo:7", func(u string) string { return `mongosh "` + u + `" --quiet --eval "40+2"` }},
		{"redis", "redis:7-alpine", func(u string) string { return `redis-cli -u "` + u + `" ECHO 42` }},
	}
	var pg dbTunnel
	top := t
	for _, e := range engines {
		t.Run(e.engine, func(t *testing.T) {
			var db struct{ ID string }
			name := "tunnel-" + e.engine
			postJSON(t, c, base+"/api/databases", fmt.Sprintf(`{"engine":%q,"name":%q}`, e.engine, name), &db)
			ctr := "skiff-db-" + db.ID
			top.Cleanup(func() {
				_ = exec.Command("docker", "rm", "-f", ctr).Run()
				_ = exec.Command("docker", "volume", "rm", ctr+"-data").Run()
			})
			if out, _ := exec.Command("docker", "port", ctr).CombinedOutput(); strings.TrimSpace(string(out)) != "" {
				t.Fatalf("a private database is published: %s", out)
			}
			tun := openTunnel(t, top, env, bin, name)
			var got string
			eventually(t, 3*time.Minute, func() bool {
				got, _ = hostClient(e.image, e.client(tun.url))
				return strings.Contains(got, "42")
			})
			if e.engine == "postgres" {
				pg, pg.container = *tun, ctr
			}
		})
	}
	if pg.url == "" {
		t.Fatal("postgres never connected")
	}
	const pgImage = "postgres:16-alpine"

	t.Run("large transfers and an idle connection", func(t *testing.T) {
		var wg sync.WaitGroup
		var idle string
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Longer than the tunnel's keepalive check, so an idle connection has to survive one.
			idle, _ = hostClient(pgImage, `(echo "select 1;"; sleep 40; echo "select 2;") | psql "`+pg.url+`" -At`)
		}()
		down, err := hostClient(pgImage, `psql "`+pg.url+`" -Atc "select repeat('x', 5000000)" | wc -c`)
		if err != nil || strings.TrimSpace(down) != "5000001" {
			t.Errorf("downloading through the tunnel: %q %v", down, err)
		}
		up, err := hostClient(pgImage, `(printf "select length('"; head -c 3000000 /dev/zero | tr '\0' y; printf "');") | psql "`+pg.url+`" -At`)
		if err != nil || strings.TrimSpace(up) != "3000000" {
			t.Errorf("uploading through the tunnel: %q %v", up, err)
		}
		wg.Wait()
		if strings.Join(strings.Fields(idle), ",") != "1,2" {
			t.Errorf("an idle connection didn't survive: %q", idle)
		}
	})

	t.Run("another team's token can't reach it", func(t *testing.T) {
		var other struct{ ID string }
		postJSON(t, c, base+"/api/teams", `{"name":"other"}`, &other)
		var me struct{ Team string }
		getJSON(t, c, base+"/api/me", &me)
		post(t, c, base+"/api/auth/team", `{"team":"`+other.ID+`"}`)
		var theirs struct{ Token string }
		postJSON(t, c, base+"/api/tokens", `{"name":"other","databases":true}`, &theirs)
		post(t, c, base+"/api/auth/team", `{"team":"`+me.Team+`"}`)
		for _, path := range []string{"/connection?port=5432", "/tunnel"} {
			req, _ := http.NewRequest(http.MethodGet, base+"/api/v1/databases/tunnel-postgres"+path, nil)
			req.Header.Set("Authorization", "Bearer "+theirs.Token)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("%s answered %d to another team's token", path, resp.StatusCode)
			}
		}
	})

	t.Run("a token made for a pipeline can't reach it", func(t *testing.T) {
		var ci struct{ Token string }
		postJSON(t, c, base+"/api/tokens", `{"name":"ci"}`, &ci)
		for _, path := range []string{"/connection?port=5432", "/tunnel"} {
			req, _ := http.NewRequest(http.MethodGet, base+"/api/v1/databases/tunnel-postgres"+path, nil)
			req.Header.Set("Authorization", "Bearer "+ci.Token)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("%s answered %d", path, resp.StatusCode)
			}
		}
	})

	t.Run("activity records it once, without the password", func(t *testing.T) {
		resp, err := c.Get(base + "/api/audit")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var log bytes.Buffer
		_, _ = log.ReadFrom(resp.Body)
		if n := strings.Count(log.String(), `"database.connect"`); n != len(engines) {
			t.Errorf("%d database.connect entries for %d databases: %s", n, len(engines), log.String())
		}
		if strings.Contains(log.String(), urlPassword(pg.url)) {
			t.Error("the password reached the activity log")
		}
	})

	t.Run("a stopped database explains itself", func(t *testing.T) {
		if out, err := exec.Command("docker", "stop", pg.container).CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		_, _ = hostClient(pgImage, `psql "`+pg.url+`" -Atc "select 1"`)
		eventually(t, 15*time.Second, func() bool { return strings.Contains(pg.errors(), "isn't running") })
		if out, err := exec.Command("docker", "start", pg.container).CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		eventually(t, time.Minute, func() bool {
			got, _ := hostClient(pgImage, `psql "`+pg.url+`" -Atc "select 42"`)
			return strings.Contains(got, "42")
		})
	})

	t.Run("revoking the token closes its tunnels", func(t *testing.T) {
		var out string
		done := make(chan struct{})
		go func() {
			defer close(done)
			out, _ = hostClient(pgImage, `(echo "select 1;"; sleep 50; echo "select 2;") | psql "`+pg.url+`" -At 2>&1`)
		}()
		time.Sleep(5 * time.Second)
		req, _ := http.NewRequest(http.MethodDelete, base+"/api/tokens?id="+token.ID, nil)
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		<-done
		if slices.Contains(strings.Fields(out), "2") {
			t.Errorf("a revoked token's connection kept working: %q", out)
		}
		_, _ = hostClient(pgImage, `psql "`+pg.url+`" -Atc "select 1"`)
		eventually(t, 15*time.Second, func() bool { return strings.Contains(pg.errors(), "skiff login") })
	})
}

type dbTunnel struct {
	url       string
	container string
	errors    func() string
}

func openTunnel(t, owner *testing.T, env []string, bin, name string) *dbTunnel {
	t.Helper()
	var out, errs safeBuffer
	cmd := exec.Command(bin, "db", "connect", name)
	cmd.Env, cmd.Stdout, cmd.Stderr = append(os.Environ(), env...), &out, &errs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	owner.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	address := regexp.MustCompile(`[a-z]+://\S+`)
	tun := &dbTunnel{errors: errs.String}
	eventually(t, 15*time.Second, func() bool {
		tun.url = address.FindString(out.String())
		return tun.url != ""
	})
	if !strings.Contains(tun.url, "@127.0.0.1:") {
		t.Fatalf("the tunnel's address isn't local: %s", out.String())
	}
	return tun
}

// hostClient runs a database's own client on this machine's network, as a desktop tool would connect.
func hostClient(image, script string) (string, error) {
	out, err := exec.Command("docker", "run", "--rm", "--network", "host", image, "sh", "-c", script).CombinedOutput()
	return string(out), err
}

func urlPassword(raw string) string {
	p, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	pass, _ := p.User.Password()
	return pass
}

type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
