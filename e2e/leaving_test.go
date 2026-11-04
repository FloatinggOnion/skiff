//go:build e2e

package e2e

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLeaving takes everything out of Skiff, removes Skiff and its containers, and runs the app and its data on plain Docker.
func TestLeaving(t *testing.T) {
	b := newDrillBox(t)
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", "left-db", "left-app").Run()
		_ = exec.Command("docker", "volume", "rm", "-f", "left-data").Run()
		_ = exec.Command("sh", "-c", "docker volume rm -f $(docker volume ls -q --filter label=skiff.volume.app="+app+")").Run()
	})
	b.deployFirst()
	put(t, b.c, b.url("/api/env?app="+app), `{"vars":[{"key":"LEFT_SECRET","value":"kept value=1","build":false}]}`)
	post(t, b.c, b.url("/api/volumes?app="+app), `{"name":"data","path":"/data"}`)
	stream(t, b.c, b.url("/api/redeploy?app="+app))
	dockerExec(t, appContainer(t, app), "echo kept > /data/note")

	d := b.database("orders")
	if out, err := d.sql("insert into t values (1), (2), (3)"); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	download := func(path, to string) {
		t.Helper()
		resp, err := b.c.Get(b.url(path))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d", path, resp.StatusCode)
		}
		f, err := os.Create(to)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, err := io.Copy(f, resp.Body); err != nil {
			t.Fatal(err)
		}
	}
	dump := filepath.Join(b.root, "orders.sql")
	var backup struct{ ID string }
	postJSON(t, b.c, b.url("/api/backups?db="+d.id), "", &backup)
	download("/api/backups/download?id="+backup.ID, dump)
	files := filepath.Join(b.root, "data.tar.gz")
	var volBackup struct{ ID string }
	postJSON(t, b.c, b.url("/api/volumes/backups?app="+app+"&volume=data"), "", &volBackup)
	download("/api/volumes/backups/download?id="+volBackup.ID, files)
	var vars []struct{ Key, Value string }
	getJSON(t, b.c, b.url("/api/env?app="+app), &vars)
	var envFile strings.Builder
	for _, v := range vars {
		fmt.Fprintf(&envFile, "%s=%s\n", v.Key, v.Value)
	}
	envPath := filepath.Join(b.root, "app.env")
	if err := os.WriteFile(envPath, []byte(envFile.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	// A bucket copies out through its own container's network, because its address is private to Skiff's.
	var bucket struct{ Name, Endpoint, AccessKey, SecretKey string }
	postJSON(t, b.c, b.url("/api/storage"), `{"name":"media"}`, &bucket)
	store := strings.TrimSuffix(strings.TrimPrefix(bucket.Endpoint, "http://"), ":8333")
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", store).Run()
		_ = exec.Command("docker", "volume", "rm", store+"-data").Run()
	})
	copied := filepath.Join(b.root, "bucket")
	aws := func(args ...string) (string, error) {
		full := append([]string{"run", "--rm", "--network", "container:" + store, "-v", copied + ":/out",
			"-e", "AWS_ACCESS_KEY_ID=" + bucket.AccessKey, "-e", "AWS_SECRET_ACCESS_KEY=" + bucket.SecretKey, "-e", "AWS_DEFAULT_REGION=us-east-1",
			"amazon/aws-cli:2.27.50", "--endpoint-url", "http://localhost:8333"}, args...)
		out, err := exec.Command("docker", full...).CombinedOutput()
		return string(out), err
	}
	if err := os.MkdirAll(copied, 0o777); err != nil {
		t.Fatal(err)
	}
	// The container writes as root, and on Linux the test can't remove root's files by itself.
	t.Cleanup(func() {
		_ = exec.Command("docker", "run", "--rm", "-v", copied+":/out", "alpine:3.20", "sh", "-c", "rm -rf /out/*").Run()
	})
	if err := os.WriteFile(filepath.Join(copied, "seed.txt"), []byte("kept"), 0o644); err != nil {
		t.Fatal(err)
	}
	eventually(t, 3*time.Minute, func() bool {
		_, err := aws("s3", "cp", "/out/seed.txt", "s3://"+bucket.Name+"/nested/seed.txt")
		return err == nil
	})
	_ = os.Remove(filepath.Join(copied, "seed.txt"))
	if out, err := aws("s3", "sync", "s3://"+bucket.Name, "/out"); err != nil {
		t.Fatalf("copying the bucket out: %v: %s", err, out)
	}
	if got, err := os.ReadFile(filepath.Join(copied, "nested", "seed.txt")); err != nil || string(got) != "kept" {
		t.Fatalf("object copied out: %q %v", got, err)
	}

	// Skiff is gone: its processes, the app's container and the database.
	b.stopPanel()
	b.stopRouter()
	shell(t, "docker rm -f $(docker ps -aq --filter label=skiff.app="+app+")")
	run(t, "", nil, "docker", "rm", "-f", d.ctr)

	run(t, "", nil, "docker", "run", "-d", "--name", "left-db", "-e", "POSTGRES_PASSWORD=x", "postgres:16-alpine")
	eventually(t, time.Minute, func() bool {
		return exec.Command("docker", "exec", "left-db", "pg_isready", "-U", "postgres").Run() == nil
	})
	time.Sleep(2 * time.Second)
	shell(t, "docker exec -i left-db psql -q -U postgres < "+dump+" 2>&1 | grep -v 'role \"skiff\" does not exist' || true")
	if out, err := exec.Command("docker", "exec", "left-db", "psql", "-U", "postgres", "-tAc", "select count(*) from t").CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "3" {
		t.Fatalf("rows in plain Postgres: %v: %s", err, out)
	}

	run(t, "", nil, "docker", "volume", "create", "left-data")
	shell(t, "docker run --rm -i -v left-data:/data alpine:3.20 tar xzf - -C /data < "+files)

	addr := freeAddr(t)
	run(t, "", nil, "docker", "run", "-d", "--name", "left-app", "--env-file", envPath, "-v", "left-data:/data", "-p", addr+":8080", "skiff-"+app+":latest")
	eventually(t, 30*time.Second, func() bool {
		resp, err := http.Get("http://" + addr + "/")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode == 200 && strings.Contains(string(body), "site v1")
	})
	if got := dockerExec(t, "left-app", "cat /data/note"); got != "kept" {
		t.Fatalf("file on the volume = %q", got)
	}
	if got := dockerExec(t, "left-app", "printenv LEFT_SECRET"); got != "kept value=1" {
		t.Fatalf("env = %q", got)
	}
}
