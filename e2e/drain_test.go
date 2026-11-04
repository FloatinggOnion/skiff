//go:build e2e

package e2e

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const drainApp = `import signal, sys, time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse, parse_qs

VERSION = open("/version").read().strip()

class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    def log_message(self, *args): pass
    def do_GET(self):
        url = urlparse(self.path)
        if url.path != "/stream":
            body = VERSION.encode()
            self.send_response(200)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Connection", "close")
        self.end_headers()
        self.close_connection = True
        for i in range(int(parse_qs(url.query)["n"][0])):
            self.wfile.write(f"{VERSION} {i}\n".encode())
            self.wfile.flush()
            time.sleep(1)
        self.wfile.write(f"{VERSION} done\n".encode())

signal.signal(signal.SIGTERM, lambda *_: sys.exit(0))
time.sleep(3)
ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
`

// TestDeployLetsStreamsFinish replaces a version during a long stream and under steady requests: the stream runs to its end, no request fails, and the old container goes once it's finished. A time limit then ends a stream that outlasts it.
func TestDeployLetsStreamsFinish(t *testing.T) {
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

	const app = "e2e-drain"
	t.Cleanup(func() {
		_ = exec.Command("sh", "-c", "docker rm -f $(docker ps -aq --filter label=skiff.app="+app+") ; docker rmi $(docker images -q skiff-"+app+")").Run()
	})
	dir := filepath.Join(root, "app")
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write("server.py", drainApp)
	write("Dockerfile", "FROM python:3.12-alpine\nCOPY server.py version /\nCMD [\"python\", \"/server.py\"]\n")
	write("skiff.toml", "name = \""+app+"\"\n\n[build]\nport = 8080\n")
	deploy := func(version string) {
		t.Helper()
		write("version", version)
		run(t, dir, env, bin, "deploy")
	}
	get := func(path string) (*http.Response, error) {
		req, _ := http.NewRequest(http.MethodGet, "http://"+routerAddr+path, nil)
		req.Host = app + ".localhost"
		return http.DefaultClient.Do(req)
	}
	answer := func() (int, string) {
		resp, err := get("/")
		if err != nil {
			return 0, err.Error()
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}
	containers := func() int {
		out, _ := exec.Command("docker", "ps", "-q", "--filter", "label=skiff.app="+app).Output()
		return len(strings.Fields(string(out)))
	}
	follow := func(n int) chan []string {
		t.Helper()
		resp, err := get(fmt.Sprintf("/stream?n=%d", n))
		if err != nil {
			t.Fatal(err)
		}
		lines := make(chan []string, 1)
		go func() {
			defer resp.Body.Close()
			var got []string
			for s := bufio.NewScanner(resp.Body); s.Scan(); {
				got = append(got, s.Text())
			}
			lines <- got
		}()
		return lines
	}

	deploy("v1")
	eventually(t, 30*time.Second, func() bool {
		code, body := answer()
		return code == 200 && body == "v1"
	})

	var failed, total atomic.Int64
	var firstFailure atomic.Value
	stop := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			select {
			case <-stop:
				return
			case <-time.After(50 * time.Millisecond):
			}
			total.Add(1)
			if code, body := answer(); code != 200 {
				failed.Add(1)
				firstFailure.CompareAndSwap(nil, fmt.Sprintf("%d: %s", code, body))
			}
		}
	}()

	stream := follow(40)
	deployed := time.Now()
	deploy("v2")
	t.Logf("deploy took %s", time.Since(deployed).Round(time.Second))
	// The router's table refreshes every couple of seconds, so both versions can answer until then.
	time.Sleep(4 * time.Second)
	for range 20 {
		if code, body := answer(); code != 200 || body != "v2" {
			t.Fatalf("after the deploy: %d %q", code, body)
		}
	}
	got := <-stream
	if len(got) != 41 || got[40] != "v1 done" {
		t.Fatalf("the stream was cut: %d lines, ending %v", len(got), got[max(0, len(got)-1):])
	}
	eventually(t, 20*time.Second, func() bool { return containers() == 1 })
	close(stop)
	<-stopped
	t.Logf("%d requests during the deploy, %d failed", total.Load(), failed.Load())
	if failed.Load() > 0 {
		t.Fatalf("%d of %d requests failed during the deploy, first: %v", failed.Load(), total.Load(), firstFailure.Load())
	}

	// A stream can't hold the old version past the limit.
	write("skiff.toml", "name = \""+app+"\"\n\n[build]\nport = 8080\n\n[deploy]\ndrain_timeout = 5\n")
	long := follow(600)
	deploy("v3")
	began := time.Now()
	cut := <-long
	if n := len(cut); n == 0 || n > 120 {
		t.Fatalf("the stream sent %d lines before the limit ended it", n)
	}
	eventually(t, 30*time.Second, func() bool { return containers() == 1 })
	t.Logf("limit ended the stream %s after the deploy", time.Since(began).Round(time.Second))
}
