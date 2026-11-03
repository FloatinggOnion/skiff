//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sharedStack = `services:
  api:
    image: alpine:3.20
    command: sleep 100000
  other:
    image: alpine:3.20
    command: sleep 100000
`

// TestStackUsesTeamResources attaches a team database and bucket to a stack service, and checks access comes and goes with the attachment.
func TestStackUsesTeamResources(t *testing.T) {
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
	base := "http://" + panel

	send := func(method, url string) {
		t.Helper()
		req, _ := http.NewRequest(method, base+url, nil)
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			t.Fatalf("%s %s: %d", method, url, resp.StatusCode)
		}
	}

	const stack = "e2e-share"
	var db struct{ ID, Host string }
	postJSON(t, c, base+"/api/databases", `{"name":"e2e-shared-db","engine":"postgres"}`, &db)
	var bucket struct{ ID string }
	postJSON(t, c, base+"/api/storage", `{"name":"e2e-shared-files"}`, &bucket)
	stackGone := false
	t.Cleanup(func() {
		if !stackGone {
			send(http.MethodDelete, "/api/stacks?name="+stack)
		}
		send(http.MethodDelete, "/api/databases?id="+db.ID)
		send(http.MethodDelete, "/api/storage?id="+bucket.ID)
	})
	quoted := strings.NewReplacer(`"`, `\"`, "\n", `\n`).Replace(sharedStack)
	post(t, c, base+"/api/stacks", fmt.Sprintf(`{"name":%q,"compose":"%s"}`, stack, quoted))

	api := stack + "-api"
	var ctr string
	eventually(t, 2*time.Minute, func() bool {
		out, _ := exec.Command("docker", "ps", "-q", "--filter", "label=skiff.app="+api).Output()
		ctr = strings.TrimSpace(string(out))
		return ctr != ""
	})
	reaches := func(container, host, port string) bool {
		t.Helper()
		return dockerExec(t, container, "nc -z -w 3 "+host+" "+port+" && echo open || echo closed") == "open"
	}
	if reaches(ctr, db.Host, "5432") {
		t.Fatal("a stack reached a team database nobody attached")
	}

	send(http.MethodPost, "/api/databases/attach?id="+db.ID+"&app="+api)
	send(http.MethodPost, "/api/storage/attach?id="+bucket.ID+"&app="+api)
	stream(t, c, base+"/api/redeploy?app="+api)
	out, _ := exec.Command("docker", "ps", "-q", "--filter", "label=skiff.app="+api).Output()
	ctr = strings.TrimSpace(string(out))
	if url := dockerExec(t, ctr, "printenv DATABASE_URL"); !strings.Contains(url, db.Host) {
		t.Fatalf("DATABASE_URL = %q", url)
	}
	eventually(t, 30*time.Second, func() bool { return reaches(ctr, db.Host, "5432") })
	endpoint := dockerExec(t, ctr, "printenv S3_ENDPOINT")
	bucketHost, bucketPort, ok := strings.Cut(strings.TrimPrefix(endpoint, "http://"), ":")
	if !ok {
		t.Fatalf("S3_ENDPOINT = %q", endpoint)
	}
	eventually(t, 30*time.Second, func() bool { return reaches(ctr, bucketHost, bucketPort) })

	// Making a database public recreates its container, which mustn't cost the stack its access.
	send(http.MethodPost, "/api/databases/public?id="+db.ID+"&on=1")
	eventually(t, 30*time.Second, func() bool { return reaches(ctr, db.Host, "5432") })
	send(http.MethodPost, "/api/databases/public?id="+db.ID+"&on=0")
	eventually(t, 30*time.Second, func() bool { return reaches(ctr, db.Host, "5432") })

	send(http.MethodDelete, "/api/databases/attach?id="+db.ID+"&app="+api)
	eventually(t, 15*time.Second, func() bool { return !reaches(ctr, db.Host, "5432") })
	if !reaches(ctr, bucketHost, bucketPort) {
		t.Fatal("detaching the database also cut off the bucket")
	}

	// Deleting a stack that's still attached leaves the shared resources running and its network gone.
	send(http.MethodPost, "/api/databases/attach?id="+db.ID+"&app="+api)
	send(http.MethodDelete, "/api/stacks?name="+stack)
	stackGone = true
	if out, _ := exec.Command("docker", "network", "ls", "-q", "--filter", "name=skiff-s-"+stack).Output(); strings.TrimSpace(string(out)) != "" {
		t.Fatal("the stack's network is still there")
	}
	var dbs []struct {
		ID       string
		Attached []string
	}
	getJSON(t, c, base+"/api/databases", &dbs)
	found := false
	for _, d := range dbs {
		if d.ID == db.ID {
			found = true
			if len(d.Attached) != 0 {
				t.Fatalf("still attached to %v", d.Attached)
			}
		}
	}
	if !found {
		t.Fatal("deleting the stack removed a team database")
	}
}
