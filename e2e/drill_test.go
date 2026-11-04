//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The drills break a real panel, router, app and database, then check what came back. They run on their own schedule: TestDrillPowerCut and TestDrillFullDisk take the whole machine with them, so they only run when SKIFF_DRILLS=machine.

const drillServer = `import os, signal, sys, time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

signal.signal(signal.SIGTERM, lambda *_: sys.exit(0))
if os.path.exists("/slowstart"):
    time.sleep(15)

class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args): pass
    def do_GET(self):
        if self.path == "/crash":
            os._exit(1)
        if self.path == "/oom":
            hog = []
            while True:
                hog.append(bytearray(8 << 20))
        body = open("/index.html", "rb").read()
        self.send_response(200)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
`

type drillBox struct {
	t, top                *testing.T
	root, home, bin       string
	env                   []string
	panelAddr             string
	routerAddr            string
	c                     *http.Client
	repo                  string
	push                  func(string)
	stopPanel, stopRouter func()
	alerts                *alertInbox
}

type alertInbox struct {
	mu    sync.Mutex
	kinds []string
}

func (a *alertInbox) got(kind string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, k := range a.kinds {
		if k == kind {
			return true
		}
	}
	return false
}

func newDrillBox(t *testing.T) *drillBox {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	root := t.TempDir()
	b := &drillBox{t: t, top: t, root: root, home: filepath.Join(root, "home"), bin: filepath.Join(root, "skiff")}
	run(t, "", nil, "go", "build", "-o", b.bin, "..")
	t.Cleanup(func() {
		_ = exec.Command("sh", "-c", "docker rm -f $(docker ps -aq --filter label=skiff.app="+app+") ; docker rmi $(docker images -q skiff-"+app+")").Run()
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
	post(t, b.c, "http://"+b.panelAddr+"/api/auth/setup", `{"secret":"e2e-setup","email":"e2e@example.com","name":"E2E","password":"e2e-password-1"}`)

	b.alerts = &alertInbox{}
	inbox := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ev struct{ Kind, Detail string }
		_ = json.NewDecoder(r.Body).Decode(&ev)
		t.Logf("alert: %s: %s", ev.Kind, ev.Detail)
		b.alerts.mu.Lock()
		b.alerts.kinds = append(b.alerts.kinds, ev.Kind)
		b.alerts.mu.Unlock()
	}))
	t.Cleanup(inbox.Close)
	put(t, b.c, b.url("/api/alerts"), `{"webhookUrl":"`+inbox.URL+`"}`)
	return b
}

func (b *drillBox) url(path string) string { return "http://" + b.panelAddr + path }

// source writes the app into the repo, leaving it to push to commit it. extra runs during the build.
func (b *drillBox) source(extra string) {
	b.t.Helper()
	work := filepath.Join(b.root, "work")
	files := map[string]string{
		"server.py":  drillServer,
		"Dockerfile": "FROM python:3.12-alpine\nCOPY server.py index.html /\n" + extra + "CMD [\"python\", \"-u\", \"/server.py\"]\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(work, name), []byte(content), 0o644); err != nil {
			b.t.Fatal(err)
		}
	}
}

func (b *drillBox) startPanel() {
	b.t.Helper()
	b.stopPanel = b.keep("panel", "--addr", b.panelAddr, "--domain", "localhost")
	waitHTTP(b.t, b.url("/api/me"))
}

func (b *drillBox) startRouter() {
	b.t.Helper()
	b.stopRouter = b.keep("router", "--domain", "localhost", "--http-addr", b.routerAddr, "--panel", b.panelAddr)
	eventually(b.t, 20*time.Second, func() bool {
		resp, err := http.Get("http://" + b.routerAddr + "/")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return true
	})
}

// keep starts a process logging to one file across restarts, printed if the test fails. The stop function is a kill -9.
func (b *drillBox) keep(args ...string) func() {
	b.t.Helper()
	cmd := exec.Command(b.bin, args...)
	cmd.Env = append(os.Environ(), b.env...)
	path := filepath.Join(b.root, args[0]+".log")
	logf, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		b.t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := cmd.Start(); err != nil {
		b.t.Fatal(err)
	}
	stopped := false
	stop := func() {
		if !stopped {
			stopped = true
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
			logf.Close()
		}
	}
	b.top.Cleanup(func() {
		stop()
		if b.top.Failed() {
			out, _ := os.ReadFile(path)
			b.top.Logf("%s log:\n%s", args[0], out)
		}
	})
	return stop
}

// drill runs one step, failing that step rather than the whole test's helpers.
func (b *drillBox) drill(name string, fn func(t *testing.T)) {
	b.top.Run(name, func(t *testing.T) {
		b.t = t
		defer func() { b.t = b.top }()
		fn(t)
	})
}

func (b *drillBox) deployFirst() {
	b.t.Helper()
	stream(b.t, b.c, b.url("/api/deploy?name="+app+"&port=8080&git="+b.repo))
	expectServed(b.t, b.routerAddr, "v1")
}

func (b *drillBox) get(path string) (int, string) {
	req, _ := http.NewRequest(http.MethodGet, "http://"+b.routerAddr+path, nil)
	req.Host = app + ".localhost"
	cl := http.Client{Timeout: 5 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// steady sends requests until the returned function is called, which reports how many there were and how many failed.
func (b *drillBox) steady() func() (int64, int64, string) {
	var total, failed atomic.Int64
	var first atomic.Value
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			case <-time.After(50 * time.Millisecond):
			}
			total.Add(1)
			if code, body := b.get("/"); code != 200 {
				failed.Add(1)
				first.CompareAndSwap(nil, fmt.Sprintf("%d %s", code, body))
			}
		}
	}()
	return func() (int64, int64, string) {
		close(stop)
		<-done
		f, _ := first.Load().(string)
		return total.Load(), failed.Load(), f
	}
}

func (b *drillBox) appContainers() []string {
	out, _ := exec.Command("docker", "ps", "-q", "--filter", "label=skiff.app="+app).Output()
	return strings.Fields(string(out))
}

func (b *drillBox) deployStatuses() []string {
	b.t.Helper()
	var project struct {
		Deploys []struct {
			Status string `json:"status"`
		} `json:"deploys"`
	}
	getJSON(b.t, b.c, b.url("/api/project?app="+app), &project)
	var out []string
	for _, d := range project.Deploys {
		out = append(out, d.Status)
	}
	return out
}

// settled waits for no deploy to be left building.
func (b *drillBox) settled(limit time.Duration) []string {
	b.t.Helper()
	var statuses []string
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		statuses = b.deployStatuses()
		if !strings.Contains(strings.Join(statuses, ","), "building") {
			return statuses
		}
		time.Sleep(time.Second)
	}
	b.t.Fatalf("a deploy is still shown as building after %s: %v", limit, statuses)
	return nil
}

// killProcess is a kill -9 of a container's main process from outside it, which is how a crash looks to Docker. A docker kill would count as stopping it on purpose.
func killProcess(t *testing.T, container string) {
	t.Helper()
	out, err := exec.Command("docker", "inspect", "-f", "{{.State.Pid}}", container).Output()
	if err != nil {
		t.Fatal(err)
	}
	run(t, "", nil, "docker", "run", "--rm", "--pid=host", "alpine:3.20", "kill", "-9", strings.TrimSpace(string(out)))
}

type drillDB struct {
	t             *testing.T
	id, ctr, name string
	mu            sync.Mutex
	acked         []int
	stop          chan struct{}
	stopped       chan struct{}
}

func (b *drillBox) database(name string) *drillDB {
	b.t.Helper()
	var db struct{ ID string }
	postJSON(b.t, b.c, b.url("/api/databases"), `{"engine":"postgres","name":"`+name+`"}`, &db)
	d := &drillDB{t: b.t, id: db.ID, ctr: "skiff-db-" + db.ID, name: name}
	b.top.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", d.ctr).Run()
		_ = exec.Command("sh", "-c", "docker volume rm -f $(docker volume ls -q --filter name="+d.ctr+")").Run()
	})
	eventually(b.t, 90*time.Second, func() bool {
		_, err := d.sql("create table if not exists t(i int)")
		return err == nil
	})
	return d
}

func (d *drillDB) sql(stmt string) (string, error) {
	out, err := exec.Command("docker", "exec", d.ctr, "psql", "-U", "skiff", "-d", d.name, "-v", "ON_ERROR_STOP=1", "-qtAc", stmt).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// write inserts numbered rows one at a time and remembers each one Postgres confirmed.
func (d *drillDB) write() {
	d.stop, d.stopped = make(chan struct{}), make(chan struct{})
	go func() {
		defer close(d.stopped)
		for i := 1; ; i++ {
			select {
			case <-d.stop:
				return
			default:
			}
			if _, err := d.sql(fmt.Sprintf("insert into t values (%d)", i)); err == nil {
				d.mu.Lock()
				d.acked = append(d.acked, i)
				d.mu.Unlock()
			} else {
				time.Sleep(200 * time.Millisecond)
			}
		}
	}()
}

func (d *drillDB) ackedCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.acked)
}

// expectEveryConfirmedRow stops the writer and fails if the database lost a row it had confirmed.
func (d *drillDB) expectEveryConfirmedRow() {
	d.t.Helper()
	close(d.stop)
	<-d.stopped
	var out string
	eventually(d.t, 2*time.Minute, func() bool {
		var err error
		out, err = d.sql("select i from t")
		return err == nil
	})
	have := map[int]bool{}
	for _, f := range strings.Fields(out) {
		n, _ := strconv.Atoi(f)
		have[n] = true
	}
	lost := 0
	for _, i := range d.acked {
		if !have[i] {
			lost++
		}
	}
	d.t.Logf("%d rows confirmed, %d lost", len(d.acked), lost)
	if lost > 0 || len(d.acked) == 0 {
		d.t.Fatalf("lost %d of %d confirmed rows", lost, len(d.acked))
	}
}

// TestDrillCrashes kills each part in turn on one running server. They're ordered so each starts from what the last one left behind.
func TestDrillCrashes(t *testing.T) {
	b := newDrillBox(t)
	b.deployFirst()

	b.drill("app crashes", func(t *testing.T) {
		began := time.Now()
		b.get("/crash")
		time.Sleep(200 * time.Millisecond)
		eventually(t, time.Minute, func() bool {
			code, body := b.get("/")
			return code == 200 && strings.Contains(body, "site v1")
		})
		t.Logf("back after %s", time.Since(began).Round(100*time.Millisecond))
	})

	b.drill("database process killed during writes", func(t *testing.T) {
		d := b.database("drill")
		d.write()
		eventually(t, 30*time.Second, func() bool { return d.ackedCount() > 20 })
		began := time.Now()
		killProcess(t, d.ctr)
		before := d.ackedCount()
		eventually(t, 2*time.Minute, func() bool { return d.ackedCount() > before+5 })
		t.Logf("writing again after %s", time.Since(began).Round(100*time.Millisecond))
		d.expectEveryConfirmedRow()
	})

	b.drill("router killed", func(t *testing.T) {
		before := strings.Join(b.appContainers(), ",")
		b.stopRouter()
		time.Sleep(2 * time.Second)
		if code, _ := b.get("/"); code != 0 {
			t.Fatalf("something still answers on the router's port: %d", code)
		}
		began := time.Now()
		b.startRouter()
		eventually(t, 30*time.Second, func() bool {
			code, body := b.get("/")
			return code == 200 && strings.Contains(body, "site v1")
		})
		t.Logf("serving again %s after the router started", time.Since(began).Round(100*time.Millisecond))
		if after := strings.Join(b.appContainers(), ","); after != before {
			t.Fatalf("the app's containers changed: %s, then %s", before, after)
		}
	})

	b.drill("panel killed while idle", func(t *testing.T) {
		finish := b.steady()
		time.Sleep(2 * time.Second)
		b.stopPanel()
		time.Sleep(30 * time.Second)
		b.startPanel()
		time.Sleep(5 * time.Second)
		total, failed, first := finish()
		t.Logf("%d requests, %d failed", total, failed)
		if failed > 0 {
			t.Fatalf("%d of %d requests failed, first: %s", failed, total, first)
		}
	})

	b.drill("panel killed during a build", func(t *testing.T) {
		b.source("RUN sleep 30\n")
		b.push("v2")
		go func() {
			resp, err := b.c.Get(b.url("/api/redeploy?app=" + app))
			if err == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		}()
		eventually(t, 30*time.Second, func() bool {
			return strings.Contains(strings.Join(b.deployStatuses(), ","), "building")
		})
		time.Sleep(5 * time.Second)
		finish := b.steady()
		b.stopPanel()
		time.Sleep(5 * time.Second)
		b.startPanel()
		statuses := b.settled(2 * time.Minute)
		t.Logf("deploys after the restart: %v", statuses)
		if statuses[0] == "live" {
			expectServed(t, b.routerAddr, "v2")
		}
		time.Sleep(5 * time.Second)
		total, failed, first := finish()
		t.Logf("%d requests, %d failed", total, failed)
		if failed > 0 {
			t.Fatalf("%d of %d requests failed, first: %s", failed, total, first)
		}
		if _, body := b.get("/"); !strings.Contains(body, "site v1") && !strings.Contains(body, "site v2") {
			t.Fatalf("serving %q", body)
		}
		b.source("")
		b.push("v3")
		stream(t, b.c, b.url("/api/redeploy?app="+app))
		expectServed(t, b.routerAddr, "v3")
		eventually(t, 30*time.Second, func() bool { return len(b.appContainers()) == 1 })
	})

	b.drill("panel killed while the new version starts", func(t *testing.T) {
		before := b.appContainers()
		b.source("RUN touch /slowstart\n")
		b.push("v4")
		go func() {
			resp, err := b.c.Get(b.url("/api/redeploy?app=" + app))
			if err == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		}()
		eventually(t, 2*time.Minute, func() bool { return len(b.appContainers()) > len(before) })
		finish := b.steady()
		b.stopPanel()
		time.Sleep(5 * time.Second)
		b.startPanel()
		statuses := b.settled(3 * time.Minute)
		t.Logf("deploys after the restart: %v", statuses)
		if statuses[0] != "live" {
			t.Errorf("the deploy didn't finish: %v", statuses)
		}
		expectServed(t, b.routerAddr, "v4")
		eventually(t, 5*time.Minute, func() bool { return len(b.appContainers()) == 1 })
		total, failed, first := finish()
		t.Logf("%d requests, %d failed", total, failed)
		if failed > 0 {
			t.Fatalf("%d of %d requests failed, first: %s", failed, total, first)
		}
		b.source("")
		b.push("v5")
		stream(t, b.c, b.url("/api/redeploy?app="+app))
		expectServed(t, b.routerAddr, "v5")
	})

	b.drill("panel killed during a backup", func(t *testing.T) {
		d := b.database("big")
		if out, err := d.sql("create table big as select i, md5(i::text) || md5((i*2)::text) as v from generate_series(1, 3000000) i"); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		backups := func() []struct{ ID string } {
			var list []struct{ ID string }
			getJSON(t, b.c, b.url("/api/backups?db="+d.id), &list)
			return list
		}
		dir := filepath.Join(b.home, ".skiff", "backups", d.id)
		go func() {
			resp, err := b.c.Post(b.url("/api/backups?db="+d.id), "application/json", nil)
			if err == nil {
				resp.Body.Close()
			}
		}()
		eventually(t, 30*time.Second, func() bool {
			files, _ := os.ReadDir(dir)
			return len(files) > 0
		})
		time.Sleep(300 * time.Millisecond)
		b.stopPanel()
		time.Sleep(2 * time.Second)
		b.startPanel()
		list := backups()
		t.Logf("after the restart: %+v", list)
		for _, row := range list {
			got := checkBackup(t, b.c, b.panelAddr, d.id, row.ID)
			if got.CheckStatus != "passed" {
				t.Fatalf("a listed backup doesn't restore: %+v", got)
			}
		}
		post(t, b.c, b.url("/api/backups?db="+d.id), "")
		eventually(t, 2*time.Minute, func() bool {
			fresh := backups()
			return len(fresh) > len(list)
		})
	})

	b.drill("app runs out of memory", func(t *testing.T) {
		// Last, because a memory limit stays with the project.
		settings := map[string]any{}
		getJSON(t, b.c, b.url("/api/project?app="+app), &settings)
		settings["memory"] = "64m"
		body, _ := json.Marshal(settings)
		put(t, b.c, b.url("/api/project?app="+app), string(body))
		stream(t, b.c, b.url("/api/redeploy?app="+app))
		expectServed(t, b.routerAddr, "v5")
		began := time.Now()
		b.get("/oom")
		time.Sleep(200 * time.Millisecond)
		eventually(t, time.Minute, func() bool {
			code, body := b.get("/")
			return code == 200 && strings.Contains(body, "site v5")
		})
		t.Logf("back after %s", time.Since(began).Round(100*time.Millisecond))
	})
}

// TestDrillBucketOutage takes the offsite bucket away while backups and history are being copied to it, then brings it back.
func TestDrillBucketOutage(t *testing.T) {
	b := newDrillBox(t)
	s3Name := "skiff-e2e-s3-drill"
	s3Port := freeAddr(t)[len("127.0.0.1:"):]
	run(t, "", nil, "docker", "run", "-d", "--name", s3Name, "-p", "127.0.0.1:"+s3Port+":8333",
		"-e", "AWS_ACCESS_KEY_ID=e2euser", "-e", "AWS_SECRET_ACCESS_KEY=e2epass12345",
		"chrislusf/seaweedfs:4.50", "server", "-dir=/data", "-s3", "-s3.port=8333")
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", s3Name).Run() })
	eventually(t, 2*time.Minute, func() bool {
		out, _ := exec.Command("docker", "exec", s3Name, "sh", "-c", "echo s3.bucket.create -name offsite | weed shell").CombinedOutput()
		return strings.Contains(string(out), "created bucket")
	})
	bucketReady := func() bool {
		resp, err := http.Get("http://127.0.0.1:" + s3Port + "/")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return true
	}

	d := b.database("orders")
	put(t, b.c, b.url("/api/backups/offsite"), fmt.Sprintf(
		`{"endpoint":"http://127.0.0.1:%s","bucket":"offsite","prefix":"skiff","accessKey":"e2euser","secretKey":"e2epass12345"}`, s3Port))

	pitr := b.url("/api/databases/pitr?db=" + d.id)
	var status struct {
		WindowStart  int64  `json:"windowStart"`
		OffsiteAt    int64  `json:"offsiteAt"`
		OffsiteError string `json:"offsiteError"`
	}
	postJSON(t, b.c, pitr, `{"action":"enable"}`, &status)
	eventually(t, 3*time.Minute, func() bool {
		getJSON(t, b.c, pitr, &status)
		return status.WindowStart > 0 && status.OffsiteAt > 0
	})
	eventually(t, time.Minute, func() bool {
		_, err := d.sql("insert into t values (1)")
		return err == nil
	})

	type backup struct {
		ID      string `json:"id"`
		Offsite string `json:"offsite"`
	}
	backups := func() []backup {
		var list []backup
		getJSON(t, b.c, b.url("/api/backups?db="+d.id), &list)
		return list
	}
	before := len(backups())

	run(t, "", nil, "docker", "stop", s3Name)
	post(t, b.c, b.url("/api/backups?db="+d.id), "")
	began := time.Now()
	eventually(t, 2*time.Minute, func() bool {
		list := backups()
		return len(list) > before && list[0].Offsite == "failed"
	})
	t.Logf("the backup was kept on the server and marked after %s", time.Since(began).Round(time.Second))
	failedID := backups()[0].ID
	eventually(t, 3*time.Minute, func() bool { return b.alerts.got("backup.offsite_failed") })

	if _, err := d.sql("insert into t values (2); select pg_switch_wal()"); err != nil {
		t.Fatal(err)
	}
	// A short blip isn't worth an alert, so history only reports a problem that lasts.
	outage := time.Now()
	eventually(t, 14*time.Minute, func() bool {
		getJSON(t, b.c, pitr, &status)
		return status.OffsiteError != "" && b.alerts.got("database.recovery_failed")
	})
	t.Logf("history reported the outage after %s: %s", time.Since(outage).Round(time.Second), status.OffsiteError)
	resp, err := b.c.Get(b.url("/api/databases"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("panel during the outage: %d", resp.StatusCode)
	}

	run(t, "", nil, "docker", "start", s3Name)
	eventually(t, time.Minute, bucketReady)
	back := time.Now()
	written := back.Unix()
	if _, err := d.sql("insert into t values (3); select pg_switch_wal()"); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Minute, func() bool {
		getJSON(t, b.c, pitr, &status)
		return status.OffsiteError == "" && status.OffsiteAt > written
	})
	t.Logf("history caught up after %s", time.Since(back).Round(time.Second))
	eventually(t, 15*time.Minute, func() bool {
		for _, row := range backups() {
			if row.ID == failedID {
				return row.Offsite == "ok"
			}
		}
		return false
	})
	t.Logf("the backup reached the bucket %s after it came back", time.Since(back).Round(time.Second))
}

func machineDrill(t *testing.T) {
	t.Helper()
	if os.Getenv("SKIFF_DRILLS") != "machine" {
		t.Skip("set SKIFF_DRILLS=machine on a machine you can lose")
	}
}

func shell(t *testing.T, script string) string {
	t.Helper()
	out, err := exec.Command("sh", "-c", script).CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v: %s", script, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestDrillPowerCut kills Docker, every container, the panel and the router at once with no warning, as closely as a test can come to losing power, then starts what a boot would start. Files the machine had written stay, so it says nothing about a disk losing them.
func TestDrillPowerCut(t *testing.T) {
	machineDrill(t)
	b := newDrillBox(t)
	b.deployFirst()
	d := b.database("orders")
	d.write()
	eventually(t, 30*time.Second, func() bool { return d.ackedCount() > 20 })

	b.stopPanel()
	b.stopRouter()
	pids := shell(t, "docker ps -q | xargs docker inspect -f '{{.State.Pid}}' | tr '\\n' ' '")
	shell(t, "sudo systemctl kill -s SIGKILL docker.service containerd.service; sudo pkill -9 -f '[c]ontainerd-shim'; sudo kill -9 "+pids+" ; true")
	before := d.ackedCount()
	shell(t, "sudo systemctl reset-failed docker.service containerd.service; sudo systemctl start containerd.service docker.service")
	began := time.Now()
	eventually(t, time.Minute, func() bool { return exec.Command("docker", "ps").Run() == nil })
	b.startPanel()
	b.startRouter()
	eventually(t, 2*time.Minute, func() bool {
		code, body := b.get("/")
		return code == 200 && strings.Contains(body, "site v1")
	})
	t.Logf("the app answered %s after Docker started", time.Since(began).Round(time.Second))
	eventually(t, 2*time.Minute, func() bool { return d.ackedCount() > before+5 })
	t.Logf("the database was writing %s after Docker started", time.Since(began).Round(time.Second))
	d.expectEveryConfirmedRow()

	b.source("")
	b.push("v2")
	stream(t, b.c, b.url("/api/redeploy?app="+app))
	expectServed(t, b.routerAddr, "v2")
}

// TestDrillFullDisk leaves no free bytes on the disk that holds Docker and Skiff's own files, pushes the app's database and a backup into it, then frees the space and checks nothing stayed broken.
func TestDrillFullDisk(t *testing.T) {
	machineDrill(t)
	b := newDrillBox(t)
	b.deployFirst()
	d := b.database("orders")
	if out, err := d.sql("create table big as select i, md5(i::text) || md5((i*2)::text) as v from generate_series(1, 1500000) i"); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	d.write()
	eventually(t, 30*time.Second, func() bool { return d.ackedCount() > 20 })
	type backup struct{ ID string }
	backups := func() []backup {
		var list []backup
		getJSON(t, b.c, b.url("/api/backups?db="+d.id), &list)
		return list
	}
	post(t, b.c, b.url("/api/backups?db="+d.id), "")
	eventually(t, 2*time.Minute, func() bool { return len(backups()) > 0 })
	count := len(backups())

	ballast := "/var/tmp/skiff-drill-ballast"
	release := func() {
		_ = exec.Command("sudo", "sh", "-c", "rm -f "+ballast+"*").Run()
		_ = exec.Command("docker", "exec", d.ctr, "rm", "-f", "/tmp/fill").Run()
	}
	t.Cleanup(release)
	shell(t, `sudo fallocate -l $(( $(df --output=avail -B1 /var/tmp | tail -1) - 4096 )) `+ballast+` ; i=0; for size in 64M 8M 1M 64K 4K; do while sudo fallocate -l $size `+ballast+`.$i 2>/dev/null; do i=$((i+1)); done; done; true`)
	// What fallocate can't reach still fills up with real writes, from each side that writes.
	userFill, _ := exec.Command("sh", "-c", "dd if=/dev/zero of="+ballast+".user bs=64k 2>&1 | tail -1").CombinedOutput()
	t.Logf("written as the panel's user before it failed: %s", strings.TrimSpace(string(userFill)))
	dbFill, _ := exec.Command("sh", "-c", "docker exec "+d.ctr+" dd if=/dev/zero of=/tmp/fill bs=64k 2>&1 | tail -1").CombinedOutput()
	t.Logf("written inside a container before it failed: %s", strings.TrimSpace(string(dbFill)))
	t.Logf("disk: %s", shell(t, "df -h /var/tmp | tail -1"))

	finish := b.steady()
	time.Sleep(20 * time.Second)

	out, err := d.sql("create table bulk as select i from generate_series(1, 2000000) i")
	t.Logf("a large write to the database: err=%v %s", err, out)
	if err == nil {
		t.Errorf("a large write claimed to work with no disk left")
	}
	if resp, err := b.c.Get(b.url("/api/databases")); err != nil {
		t.Errorf("the panel stopped answering: %v", err)
	} else {
		resp.Body.Close()
		t.Logf("panel reads: %d", resp.StatusCode)
	}
	if resp, err := b.c.Post(b.url("/api/backups?db="+d.id), "application/json", nil); err != nil {
		t.Errorf("taking a backup: %v", err)
	} else {
		msg, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Logf("taking a backup: %d %s", resp.StatusCode, msg)
	}
	ok, log := streamResult(t, b.c, b.url("/api/redeploy?app="+app))
	t.Logf("deploying: ok=%v\n%s", ok, log)
	if ok {
		t.Error("a deploy claimed to work with no disk left")
	}
	if !strings.Contains(log, "disk space") {
		t.Error("the failed deploy didn't mention the disk")
	}
	total, failed, first := finish()
	t.Logf("the app with a full disk: %d requests, %d failed", total, failed)
	if failed > 0 {
		t.Errorf("%d of %d requests failed, first: %s", failed, total, first)
	}

	release()
	t.Logf("disk: %s", shell(t, "df -h /var/tmp | tail -1"))
	began := time.Now()
	before := d.ackedCount()
	eventually(t, 3*time.Minute, func() bool { return d.ackedCount() > before+5 })
	t.Logf("the database was writing %s after space came back", time.Since(began).Round(time.Second))
	d.expectEveryConfirmedRow()

	// Nothing listed may be a backup cut short by the full disk.
	for _, row := range backups() {
		got := checkBackup(t, b.c, b.panelAddr, d.id, row.ID)
		t.Logf("backup %s: %s %s", row.ID, got.CheckStatus, got.CheckDetail)
		if got.CheckStatus != "passed" {
			t.Errorf("a listed backup doesn't restore: %+v", got)
		}
	}
	post(t, b.c, b.url("/api/backups?db="+d.id), "")
	eventually(t, 2*time.Minute, func() bool { return len(backups()) > count })
	b.push("v2")
	stream(t, b.c, b.url("/api/redeploy?app="+app))
	expectServed(t, b.routerAddr, "v2")
}
