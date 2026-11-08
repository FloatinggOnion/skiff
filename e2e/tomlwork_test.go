//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestTomlWorkersAndJobs deploys an app whose skiff.toml lists a worker and a job, and checks the CLI runs the worker on each new version while the panel shows both and runs the job.
func TestTomlWorkersAndJobs(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
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

	const app = "e2e-toml"
	t.Cleanup(func() {
		_ = exec.Command("sh", "-c", "docker rm -f $(docker ps -aq --filter label=skiff.app="+app+") $(docker ps -aq --filter label=skiff.worker.app="+app+") ; docker rmi $(docker images -q skiff-"+app+")").Run()
	})
	dir := filepath.Join(root, "app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	deploy := func(work string) {
		t.Helper()
		toml := "name = \"" + app + "\"\n\n[build]\nimage = \"python:3.12-alpine\"\nport = 8080\n\n[deploy]\ncommand = [\"python\", \"-m\", \"http.server\", \"8080\"]\n\n" + work
		if err := os.WriteFile(filepath.Join(dir, "skiff.toml"), []byte(toml), 0o644); err != nil {
			t.Fatal(err)
		}
		run(t, dir, env, bin, "deploy")
	}
	workers := func() []string {
		out, _ := exec.Command("docker", "ps", "-q", "--filter", "label=skiff.worker.app="+app).Output()
		return strings.Fields(string(out))
	}
	// Until the panel adopts the app, these answer forbidden, which reads as empty.
	fetch := func(path string, into any) {
		resp, err := c.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			_ = json.NewDecoder(resp.Body).Decode(into)
		}
	}
	type job struct{ ID, Name, Schedule string }
	jobs := func() []job {
		var out []job
		fetch("/api/jobs?app="+app, &out)
		return out
	}
	type worker struct {
		Name     string
		Replicas int
		Running  int
	}
	listed := func() []worker {
		var out []worker
		fetch("/api/workers?app="+app, &out)
		return out
	}

	deploy("[[workers]]\nname = \"queue\"\ncommand = \"sleep 100000\"\nreplicas = 2\n\n[[jobs]]\nname = \"report\"\nschedule = \"0 3 * * *\"\ncommand = \"echo job-ran\"\n")
	if got := workers(); len(got) != 2 {
		t.Fatalf("%d workers running after the deploy, want 2", len(got))
	}
	appImage, _ := exec.Command("sh", "-c", "docker inspect -f '{{.Image}}' $(docker ps -q --filter label=skiff.app="+app+" | head -1)").Output()
	workerImage, _ := exec.Command("docker", "inspect", "-f", "{{.Image}}", workers()[0]).Output()
	if strings.TrimSpace(string(appImage)) != strings.TrimSpace(string(workerImage)) {
		t.Errorf("the worker runs %s, the app %s", workerImage, appImage)
	}

	// The panel picks the CLI app up on its next pass.
	eventually(t, 90*time.Second, func() bool {
		l := listed()
		return len(l) == 1 && l[0].Name == "queue" && l[0].Replicas == 2 && l[0].Running == 2 && len(jobs()) == 1
	})
	first := jobs()[0]
	var ran struct {
		OK     bool
		Output string
	}
	resp, err := c.Post(base+"/api/jobs/run?id="+first.ID, "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.NewDecoder(resp.Body).Decode(&ran)
	resp.Body.Close()
	if !ran.OK || !strings.Contains(ran.Output, "job-ran") {
		t.Errorf("running the job: %+v", ran)
	}
	resp, err = c.Post(base+"/api/workers?app="+app, "application/json", strings.NewReader(`{"name":"other","command":"sleep 1","replicas":1}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("adding a worker to an app whose skiff.toml owns them: %d", resp.StatusCode)
	}

	// A changed schedule keeps the job, and when it last ran.
	deploy("[[workers]]\nname = \"queue\"\ncommand = \"sleep 100000\"\n\n[[jobs]]\nname = \"report\"\nschedule = \"0 4 * * *\"\ncommand = \"echo job-ran\"\n")
	if got := workers(); len(got) != 1 {
		t.Errorf("%d workers after lowering replicas, want 1", len(got))
	}
	eventually(t, 90*time.Second, func() bool {
		j := jobs()
		return len(j) == 1 && j[0].Schedule == "0 4 * * *"
	})
	if j := jobs(); j[0].ID != first.ID {
		t.Errorf("the job was replaced, not changed: %s became %s", first.ID, j[0].ID)
	}

	deploy("")
	if got := workers(); len(got) != 0 {
		t.Errorf("%d workers still running after skiff.toml dropped them", len(got))
	}
	eventually(t, 90*time.Second, func() bool { return len(listed()) == 0 && len(jobs()) == 0 })
}
