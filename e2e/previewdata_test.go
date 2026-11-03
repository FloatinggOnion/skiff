//go:build e2e

package e2e

import (
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestPreviewData checks previews get their own databases, and that a failure never falls through to the project's data.
func TestPreviewData(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	bin := filepath.Join(root, "skiff")
	run(t, "", nil, "go", "build", "-o", bin, "..")
	repo, push := gitServer(t, root)
	push("v1")
	work := filepath.Join(root, "work")
	bare := filepath.Join(root, "srv", "site.git")
	for _, b := range []string{"copied", "blank", "broken"} {
		run(t, work, nil, "git", "branch", b)
		run(t, work, nil, "git", "push", "-q", bare, b)
	}

	protectExisting(t, home)
	panel := freeAddr(t)
	start(t, []string{"HOME=" + home, "SKIFF_PANEL_PASSWORD=e2e-setup", "SKIFF_ALLOW_PRIVATE_ENDPOINTS=1"}, bin, "panel", "--addr", panel, "--domain", "localhost")
	waitHTTP(t, "http://"+panel+"/api/me")
	c := client(t)
	post(t, c, "http://"+panel+"/api/auth/setup", `{"secret":"e2e-setup","email":"e2e@example.com","name":"E2E","password":"e2e-password-1"}`)

	const project = "e2e-pd"
	base := "http://" + panel
	var dbID string
	t.Cleanup(func() {
		script := `for p in copied blank broken; do
  for c in $(docker ps -aq --filter label=skiff.preview=` + project + `-$p); do
    n=$(docker inspect -f '{{.Name}}' $c | tr -d /); docker rm -f $c; docker volume rm $n-data
  done
  docker rm -f $(docker ps -aq --filter label=skiff.app=` + project + `-$p)
done
docker rm -f $(docker ps -aq --filter label=skiff.app=` + project + `)`
		_ = exec.Command("sh", "-c", script).Run()
		if dbID != "" {
			_ = exec.Command("docker", "rm", "-f", "skiff-db-"+dbID).Run()
			_ = exec.Command("docker", "volume", "rm", "skiff-db-"+dbID+"-data").Run()
		}
	})
	stream(t, c, base+"/api/deploy?name="+project+"&port=8080&git="+repo)

	var db struct{ ID string }
	postJSON(t, c, base+"/api/databases", `{"engine":"postgres","name":"orders"}`, &db)
	dbID = db.ID
	source := "skiff-db-" + db.ID
	eventually(t, 90*time.Second, func() bool {
		return exec.Command("docker", "exec", source, "pg_isready", "-h", "127.0.0.1", "-U", "skiff", "-d", "orders").Run() == nil
	})
	sql := func(ctr, stmt string) string {
		t.Helper()
		out, err := exec.Command("docker", "exec", ctr, "psql", "-U", "skiff", "-d", "orders", "-v", "ON_ERROR_STOP=1", "-tAc", stmt).CombinedOutput()
		if err != nil {
			t.Fatalf("%s on %s: %v: %s", stmt, ctr, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	eventually(t, 30*time.Second, func() bool {
		return exec.Command("docker", "exec", source, "psql", "-U", "skiff", "-d", "orders", "-c", "create table if not exists items(v text)").Run() == nil
	})
	sql(source, "insert into items values ('production-row')")
	post(t, c, base+"/api/databases/attach?id="+db.ID+"&app="+project, "")
	stream(t, c, base+"/api/redeploy?app="+project)

	setMode := func(mode string) {
		t.Helper()
		put(t, c, base+"/api/project/preview-data?app="+project, `{"mode":"`+mode+`"}`)
	}
	container := func(app string) string {
		out, _ := exec.Command("docker", "ps", "-q", "--filter", "label=skiff.app="+app).Output()
		return strings.TrimSpace(string(out))
	}
	previewDB := func(app string) string {
		out, _ := exec.Command("docker", "ps", "-q", "--filter", "label=skiff.preview="+app).Output()
		return strings.TrimSpace(string(out))
	}
	url := func(app string) string { return dockerExec(t, container(app), "printenv DATABASE_URL") }
	prodURL := url(project)

	// A copy starts with the project's data, and is entirely separate.
	setMode("copy")
	const copied = project + "-copied"
	stream(t, c, base+"/api/preview?app="+project+"&branch=copied")
	eventually(t, time.Minute, func() bool { return container(copied) != "" })
	pdb := previewDB(copied)
	if pdb == "" {
		t.Fatal("the preview has no database of its own")
	}
	if url(copied) == prodURL || !strings.Contains(url(copied), "@skiff-db-") {
		t.Fatalf("the preview connects to %q, the project to %q", url(copied), prodURL)
	}
	if got := sql(pdb, "select v from items"); got != "production-row" {
		t.Fatalf("the copy holds %q", got)
	}
	sql(pdb, "insert into items values ('preview-row')")
	if got := sql(source, "select count(*) from items"); got != "1" {
		t.Fatalf("writing to the preview reached the project: %s rows", got)
	}

	// Pushing to the branch keeps the preview's database and its data.
	stream(t, c, base+"/api/redeploy?app="+copied)
	if previewDB(copied) != pdb {
		t.Fatal("a redeploy replaced the preview's database")
	}
	if got := sql(pdb, "select count(*) from items"); got != "2" {
		t.Fatalf("a redeploy changed the preview's data: %s rows", got)
	}

	// An empty one has none of the project's tables.
	setMode("empty")
	const blank = project + "-blank"
	stream(t, c, base+"/api/preview?app="+project+"&branch=blank")
	eventually(t, time.Minute, func() bool { return container(blank) != "" })
	if got := sql(previewDB(blank), "select count(*) from information_schema.tables where table_name='items'"); got != "0" {
		t.Fatalf("the empty database has the project's table: %s", got)
	}

	// If a copy can't be made, nothing runs, so nothing can reach the project's database.
	setMode("copy")
	var backups []struct{ ID string }
	getJSON(t, c, base+"/api/backups?db="+db.ID, &backups)
	for _, b := range backups {
		req, _ := http.NewRequest(http.MethodDelete, base+"/api/backups?id="+b.ID, nil)
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	run(t, "", nil, "docker", "stop", source)
	const broken = project + "-broken"
	resp, err := c.Get(base + "/api/preview?app=" + project + "&branch=broken")
	if err != nil {
		t.Fatal(err)
	}
	log, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(log), "can't reach your project's data") {
		t.Fatalf("the deploy log: %s", log)
	}
	if container(broken) != "" {
		t.Fatal("a preview started without its own database")
	}
	var project2 struct {
		Deploys []struct{ Status string } `json:"deploys"`
	}
	getJSON(t, c, base+"/api/project?app="+broken, &project2)
	if len(project2.Deploys) == 0 || project2.Deploys[0].Status != "failed" {
		t.Fatalf("the deploy should be marked failed: %+v", project2.Deploys)
	}
	run(t, "", nil, "docker", "start", source)

	// A preview's database goes with it, and the project's stays.
	post(t, c, base+"/api/down?app="+copied, "")
	eventually(t, time.Minute, func() bool { return previewDB(copied) == "" && container(copied) == "" })
	out, _ := exec.Command("docker", "ps", "-q", "--filter", "name="+source).Output()
	if strings.TrimSpace(string(out)) == "" {
		t.Fatal("deleting a preview removed the project's database")
	}
	var dbs []struct{ Name string }
	getJSON(t, c, base+"/api/databases", &dbs)
	for _, d := range dbs {
		if strings.HasPrefix(d.Name, copied) {
			t.Fatalf("the preview's database is still listed: %+v", dbs)
		}
	}
}
