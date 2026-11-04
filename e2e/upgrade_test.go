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
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

// upgradeSources is the newest patch of every released minor, which is where a server upgrading from that minor would be.
func upgradeSources(t *testing.T) []string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repos/nuelScript/skiff/releases?per_page=100", nil)
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("listing releases: %d", resp.StatusCode)
	}
	var releases []struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		t.Fatal(err)
	}
	// Newest first, so the first of each minor is its newest patch.
	minor := regexp.MustCompile(`^v\d+\.\d+`)
	seen := map[string]bool{}
	var out []string
	for _, r := range releases {
		if m := minor.FindString(r.Tag); m != "" && !seen[m] {
			seen[m] = true
			out = append(out, r.Tag)
		}
	}
	if len(out) == 0 {
		t.Fatal("no releases found")
	}
	return out
}

func downloadRelease(t *testing.T, tag, to string) {
	t.Helper()
	url := fmt.Sprintf("https://github.com/nuelScript/skiff/releases/download/%s/skiff-%s-%s", tag, runtime.GOOS, runtime.GOARCH)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s: %d", url, resp.StatusCode)
	}
	f, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		t.Fatal(err)
	}
}

// TestUpgrade runs a server on each earlier minor with real state, swaps in this build the way an update does, and checks everything is still there and still works.
func TestUpgrade(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	current := filepath.Join(t.TempDir(), "skiff")
	run(t, "", nil, "go", "build", "-o", current, "..")
	for _, tag := range upgradeSources(t) {
		t.Run(tag, func(t *testing.T) { upgradeFrom(t, tag, current) })
	}
}

func upgradeFrom(t *testing.T, tag, current string) {
	root := t.TempDir()
	b := &drillBox{t: t, top: t, root: root, home: filepath.Join(root, "home"), bin: filepath.Join(root, "old")}
	downloadRelease(t, tag, b.bin)
	t.Cleanup(func() {
		_ = exec.Command("sh", "-c", "docker rm -f $(docker ps -aq --filter label=skiff.app="+app+") ; docker rmi -f $(docker images -q skiff-"+app+")").Run()
		_ = exec.Command("sh", "-c", "docker volume rm -f $(docker volume ls -q --filter label=skiff.volume.app="+app+")").Run()
	})
	b.repo, b.push = gitServer(t, root)
	b.source("")
	b.push("v1")
	protectExisting(t, b.home)
	b.env = []string{"HOME=" + b.home, "SKIFF_PANEL_PASSWORD=e2e-setup", "SKIFF_ALLOW_PRIVATE_ENDPOINTS=1"}
	b.panelAddr, b.routerAddr = freeAddr(t), freeAddr(t)
	b.startPanel()
	b.startRouter()
	b.c = client(t)
	post(t, b.c, b.url("/api/auth/setup"), `{"secret":"e2e-setup","email":"e2e@example.com","name":"E2E","password":"e2e-password-1"}`)

	// A server with something on it: an app with a secret and a volume, a database with rows, and a backup.
	const secret = "upgrade-secret-value"
	b.deployFirst()
	put(t, b.c, b.url("/api/env?app="+app), `{"vars":[{"key":"UPGRADE_SECRET","value":"`+secret+`","build":false}]}`)
	// Older versions don't have volumes.
	resp, err := b.c.Post(b.url("/api/volumes?app="+app), "application/json", strings.NewReader(`{"name":"data","path":"/data"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	hasVolume := resp.StatusCode == http.StatusCreated
	t.Logf("volumes: %v", hasVolume)
	stream(t, b.c, b.url("/api/redeploy?app="+app))
	expectServed(t, b.routerAddr, "v1")
	if hasVolume {
		dockerExec(t, appContainer(t, app), "echo kept > /data/note")
	}
	d := b.database("orders")
	if out, err := d.sql("insert into t values (1), (2), (3)"); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	var backup struct{ ID string }
	postJSON(t, b.c, b.url("/api/backups?db="+d.id), "", &backup)
	var before struct {
		Deploys []struct {
			ID string `json:"id"`
		} `json:"deploys"`
	}
	getJSON(t, b.c, b.url("/api/project?app="+app), &before)

	// An update replaces the binary and restarts the panel. The router keeps running the old code until someone restarts it.
	finish := b.steady()
	b.stopPanel()
	b.bin = current
	b.startPanel()
	eventually(t, time.Minute, func() bool {
		resp, err := b.c.Get(b.url("/api/project?app=" + app))
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})
	time.Sleep(5 * time.Second)
	total, failed, first := finish()
	t.Logf("while the panel updated: %d requests, %d failed", total, failed)
	if failed > 0 {
		t.Errorf("%d of %d requests failed while the panel updated, first: %s", failed, total, first)
	}

	// Everything from before is there, and still signed in.
	var vars []struct{ Key, Value string }
	getJSON(t, b.c, b.url("/api/env?app="+app), &vars)
	if len(vars) != 1 || vars[0].Value != secret {
		t.Errorf("the secret after upgrading: %+v", vars)
	}
	if hasVolume {
		if got := dockerExec(t, appContainer(t, app), "cat /data/note"); got != "kept" {
			t.Errorf("the volume after upgrading: %q", got)
		}
	}
	if out, err := d.sql("select count(*) from t"); err != nil || out != "3" {
		t.Errorf("rows after upgrading: %v %s", err, out)
	}
	if got := checkBackup(t, b.c, b.panelAddr, d.id, backup.ID); got.CheckStatus != "passed" {
		t.Errorf("the backup taken before upgrading: %+v", got)
	}
	raw, err := os.ReadFile(filepath.Join(b.home, ".skiff", "skiff.db"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) {
		t.Errorf("the secret is in plain text in the database after upgrading")
	}

	// A deploy works with the old router still running, and the secret reaches it.
	b.source("")
	b.push("v2")
	stream(t, b.c, b.url("/api/redeploy?app="+app))
	expectServed(t, b.routerAddr, "v2")
	if got := dockerExec(t, appContainer(t, app), "printenv UPGRADE_SECRET"); got != secret {
		t.Errorf("the secret in the new version: %q", got)
	}
	if hasVolume {
		if got := dockerExec(t, appContainer(t, app), "cat /data/note"); got != "kept" {
			t.Errorf("the volume in the new version: %q", got)
		}
	}

	// Then the router updates too, and a version from before the upgrade can still be rolled back to.
	b.stopRouter()
	b.startRouter()
	expectServed(t, b.routerAddr, "v2")
	stream(t, b.c, b.url("/api/rollback?app="+app+"&id="+before.Deploys[0].ID))
	expectServed(t, b.routerAddr, "v1")
}
