//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const upgradeStack = "upg"

// seedKinds gives the old server a worker, a scheduled job, a bucket with a file in it and a Compose stack, each only where that version has it. The returned check runs after the upgrade.
func seedKinds(t *testing.T, b *drillBox) (check func(redeployed bool)) {
	t.Helper()
	t.Cleanup(func() {
		_ = exec.Command("sh", "-c", "docker rm -f $(docker ps -aq --filter label=skiff.worker.app="+app+") $(docker ps -aq --filter label=skiff.app="+upgradeStack+"-web)").Run()
		// The stack's database keeps its data in a volume of its own.
		_ = exec.Command("sh", "-c", "ids=$(docker ps -aq --filter label=skiff.stack="+upgradeStack+"); [ -n \"$ids\" ] || exit 0; vols=$(docker inspect -f '{{range .Mounts}}{{.Name}} {{end}}' $ids); docker rm -f $ids; docker volume rm $vols").Run()
	})
	send := func(method, path, body string) (int, []byte) {
		t.Helper()
		req, _ := http.NewRequest(method, b.url(path), strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := b.c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, out
	}
	workers := func() int {
		out, _ := exec.Command("docker", "ps", "-q", "--filter", "label=skiff.worker.app="+app).Output()
		return len(strings.Fields(string(out)))
	}

	if code, out := send(http.MethodPost, "/api/workers?app="+app, `{"name":"queue","command":"sleep 100000","replicas":2}`); code != http.StatusOK {
		t.Fatalf("adding a worker: %d %s", code, out)
	}
	eventually(t, 2*time.Minute, func() bool { return workers() == 2 })

	var job struct{ ID string }
	code, out := send(http.MethodPost, "/api/jobs?app="+app, `{"name":"report","schedule":"0 3 * * *","command":"echo job-ran"}`)
	if code != http.StatusOK || json.Unmarshal(out, &job) != nil || job.ID == "" {
		t.Fatalf("adding a job: %d %s", code, out)
	}

	// The oldest versions ran buckets on an image that can no longer be pulled, so a bucket only counts where the old version can still make one.
	var bucket struct{ ID, Endpoint string }
	code, out = send(http.MethodPost, "/api/storage", `{"name":"upgrade-files"}`)
	hasBucket := code == http.StatusOK && json.Unmarshal(out, &bucket) == nil && bucket.ID != ""
	t.Logf("buckets: %v %s", hasBucket, out[:min(len(out), 120)])
	hasFile := false
	if hasBucket {
		store := strings.TrimSuffix(strings.TrimPrefix(bucket.Endpoint, "http://"), ":8333")
		t.Cleanup(func() {
			_ = exec.Command("docker", "rm", "-f", store).Run()
			_ = exec.Command("docker", "volume", "rm", store+"-data").Run()
		})
		if code, out := send(http.MethodPost, "/api/storage/attach?id="+bucket.ID+"&app="+app, ""); code >= 300 {
			t.Fatalf("attaching the bucket: %d %s", code, out)
		}
		// Older versions have no file browser.
		code, _ = send(http.MethodPut, "/api/storage/object?id="+bucket.ID+"&key=kept.txt", "kept-file")
		hasFile = code == http.StatusNoContent
	}
	t.Logf("bucket files: %v", hasFile)

	// Stacks arrived after the oldest releases, which answer an unknown address with the dashboard page.
	compose := `services:\n  web:\n    image: traefik/whoami:v1.10.2\n    ports: [\"80:80\"]\n  cache:\n    image: redis:7-alpine\n`
	code, out = send(http.MethodPost, "/api/stacks", `{"name":"`+upgradeStack+`","compose":"`+compose+`"}`)
	hasStack := code == http.StatusCreated
	if !hasStack && code >= 400 && code != http.StatusNotFound && code != http.StatusMethodNotAllowed {
		t.Fatalf("creating a stack: %d %s", code, out)
	}
	t.Logf("stacks: %v", hasStack)
	stackWeb := func() bool {
		out, _ := exec.Command("docker", "ps", "-q", "--filter", "label=skiff.app="+upgradeStack+"-web").Output()
		return len(strings.Fields(string(out))) > 0
	}
	if hasStack {
		eventually(t, 3*time.Minute, stackWeb)
	}

	return func(redeployed bool) {
		t.Helper()
		var listed []struct{ Name string }
		if code, out := send(http.MethodGet, "/api/workers?app="+app, ""); code != http.StatusOK || json.Unmarshal(out, &listed) != nil || len(listed) != 1 {
			t.Errorf("the worker after upgrading: %d %s", code, out)
		}
		eventually(t, 2*time.Minute, func() bool { return workers() == 2 })
		if redeployed {
			// Workers are replaced after the deploy finishes, so give them a moment to move to the new build.
			want, _ := exec.Command("docker", "inspect", "-f", "{{.Image}}", appContainer(t, app)).Output()
			var ids []byte
			eventually(t, 2*time.Minute, func() bool {
				ids, _ = exec.Command("sh", "-c", "docker inspect -f '{{.Image}}' $(docker ps -q --filter label=skiff.worker.app="+app+")").Output()
				f := strings.Fields(string(ids))
				return len(f) == 2 && f[0] == strings.TrimSpace(string(want)) && f[1] == f[0]
			})
			return
		}

		var ran struct {
			OK     bool
			Output string
		}
		code, out := send(http.MethodPost, "/api/jobs/run?id="+job.ID, "")
		if code != http.StatusOK || json.Unmarshal(out, &ran) != nil || !ran.OK || !strings.Contains(ran.Output, "job-ran") {
			t.Errorf("running the job after upgrading: %d %s", code, out)
		}

		var vars []struct{ Key, Value string }
		_, out = send(http.MethodGet, "/api/env?app="+app, "")
		_ = json.Unmarshal(out, &vars)
		bucketVar := false
		for _, v := range vars {
			bucketVar = bucketVar || v.Key == "S3_BUCKET" && v.Value == "upgrade-files"
		}
		if hasBucket && !bucketVar {
			t.Errorf("the bucket's variables after upgrading: %+v", vars)
		}
		if hasFile {
			code, out := send(http.MethodGet, "/api/storage/object?id="+bucket.ID+"&key=kept.txt", "")
			if code != http.StatusOK || !bytes.Equal(out, []byte("kept-file")) {
				t.Errorf("the bucket's file after upgrading: %d %q", code, out)
			}
		}

		if hasStack {
			var stacks []struct{ Name string }
			_, out := send(http.MethodGet, "/api/stacks", "")
			_ = json.Unmarshal(out, &stacks)
			if len(stacks) != 1 || stacks[0].Name != upgradeStack {
				t.Errorf("the stack after upgrading: %s", out)
			}
			if !stackWeb() {
				t.Error("the stack's web service stopped during the upgrade")
			}
		}
	}
}
