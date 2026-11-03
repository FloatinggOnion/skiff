//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestDebugging(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	bin := filepath.Join(root, "skiff")
	run(t, "", nil, "go", "build", "-o", bin, "..")
	repo, push := gitServer(t, root)
	work := filepath.Join(root, "work")
	dockerfile := func(cmd string) {
		t.Helper()
		content := "FROM busybox:1.36\nRUN mkdir /www && echo ok > /www/index.html\nCMD " + cmd + "\n"
		if err := os.WriteFile(filepath.Join(work, "Dockerfile"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dockerfile(`["httpd", "-f", "-p", "8080", "-h", "/www"]`)
	push("v1")

	protectExisting(t, home)
	panel := freeAddr(t)
	start(t, []string{"HOME=" + home, "SKIFF_PANEL_PASSWORD=e2e-setup", "SKIFF_ALLOW_PRIVATE_ENDPOINTS=1"}, bin, "panel", "--addr", panel, "--domain", "localhost")
	waitHTTP(t, "http://"+panel+"/api/me")
	c := client(t)
	post(t, c, "http://"+panel+"/api/auth/setup", `{"secret":"e2e-setup","email":"e2e@example.com","name":"E2E","password":"e2e-password-1"}`)
	const app = "e2e-dbg"
	t.Cleanup(func() {
		_ = exec.Command("sh", "-c", "docker rm -f $(docker ps -aq --filter label=skiff.app="+app+") $(docker ps -aq --filter label=skiff.worker.app="+app+"); docker rmi skiff-"+app+":latest").Run()
	})
	stream(t, c, "http://"+panel+"/api/deploy?name="+app+"&port=8080&git="+repo)
	base := "http://" + panel

	post(t, c, base+"/api/workers?app="+app, `{"name":"mailer","command":"sh -c 'while true; do echo mailer-marker; sleep 1; done'","replicas":1}`)
	var tok struct{ Token string }
	postJSON(t, c, base+"/api/tokens", `{"name":"e2e-debug"}`, &tok)

	apiLogs := func(query string) string {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, base+"/api/v1/apps/"+app+"/logs"+query, nil)
		req.Header.Set("Authorization", "Bearer "+tok.Token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var body struct{ Logs string }
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return body.Logs
	}
	eventually(t, time.Minute, func() bool { return strings.Contains(apiLogs(""), "[mailer] mailer-marker") })

	// Live output reaches both streams, from the same worker.
	for _, follow := range []struct {
		url    string
		client *http.Client
		auth   bool
	}{
		{base + "/api/logs?app=" + app, c, false},
		{base + "/api/v1/apps/" + app + "/logs?follow=1", http.DefaultClient, true},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, follow.url, nil)
		if follow.auth {
			req.Header.Set("Authorization", "Bearer "+tok.Token)
		}
		resp, err := follow.client.Do(req)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		buf := make([]byte, 4096)
		var got bytes.Buffer
		for !strings.Contains(got.String(), "[mailer] mailer-marker") {
			n, rerr := resp.Body.Read(buf)
			got.Write(buf[:n])
			if rerr != nil {
				t.Fatalf("%s: %v after %q", follow.url, rerr, got.String())
			}
		}
		resp.Body.Close()
		cancel()
	}

	// A laptop has no local copy of the app, so the CLI has to ask the server.
	laptop := filepath.Join(root, "laptop")
	if err := os.MkdirAll(filepath.Join(laptop, ".skiff"), 0o700); err != nil {
		t.Fatal(err)
	}
	creds, _ := json.Marshal(map[string]string{"url": base, "token": tok.Token})
	if err := os.WriteFile(filepath.Join(laptop, ".skiff", "credentials.json"), creds, 0o600); err != nil {
		t.Fatal(err)
	}
	cli := exec.Command(bin, "logs", app, "-n", "20")
	cli.Env = append(os.Environ(), "HOME="+laptop)
	out, err := cli.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "mailer-marker") {
		t.Fatalf("skiff logs from another machine: %v\n%s", err, out)
	}
	cli = exec.Command(bin, "logs", "e2e-no-such-app")
	cli.Env = append(os.Environ(), "HOME="+laptop)
	if out, err := cli.CombinedOutput(); err == nil || !strings.Contains(string(out), "no running app") {
		t.Fatalf("skiff logs for an unknown app: %v\n%s", err, out)
	}

	// The worker's console opens in the worker, not the app.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws://"+panel+"/api/exec?app="+app+"&worker=mailer", &websocket.DialOptions{HTTPClient: c})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	input, _ := json.Marshal(map[string]string{"t": "in", "d": "tr '\\0' ' ' </proc/1/cmdline; echo; echo end-$((40+2))\n"})
	if err := ws.Write(ctx, websocket.MessageText, input); err != nil {
		t.Fatal(err)
	}
	var shell strings.Builder
	for !strings.Contains(shell.String(), "end-42") {
		_, data, err := ws.Read(ctx)
		if err != nil {
			t.Fatalf("console: %v after %q", err, shell.String())
		}
		shell.Write(data)
	}
	if !strings.Contains(shell.String(), "mailer-marker") {
		t.Fatalf("the console didn't open in the worker:\n%s", shell.String())
	}
	ws.Close(websocket.StatusNormalClosure, "")

	resp, err := c.Get(base + "/api/exec?app=" + app + "&worker=no-such-worker")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("a console in an unknown worker: %d", resp.StatusCode)
	}

	var audit []struct{ Action, Target, Detail string }
	getJSON(t, c, base+"/api/audit", &audit)
	opened := false
	for _, a := range audit {
		opened = opened || (a.Action == "console.open" && a.Target == app && a.Detail == "worker mailer")
	}
	if !opened {
		t.Fatalf("opening a console wasn't recorded: %+v", audit)
	}

	// A crash on deploy: the log says why, though the container is gone, and the old version keeps serving.
	running := appContainer(t, app)
	var settings map[string]any
	getJSON(t, c, base+"/api/project?app="+app, &settings)
	settings["healthTimeout"] = 5
	body, _ := json.Marshal(settings)
	req, _ := http.NewRequest(http.MethodPut, base+"/api/project?app="+app, strings.NewReader(string(body)))
	resp, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("saving settings: %d %s", resp.StatusCode, msg)
	}
	dockerfile(`["sh", "-c", "echo crash-$((40+2)); exit 1"]`)
	push("v2")
	ok, log := streamResult(t, c, base+"/api/redeploy?app="+app)
	if ok || !strings.Contains(log, "crash-42") || !strings.Contains(log, "kept exiting") {
		t.Fatalf("a crashing deploy should explain itself:\n%s", log)
	}
	if appContainer(t, app) != running {
		t.Fatal("the running version was replaced by one that crashed")
	}
}
