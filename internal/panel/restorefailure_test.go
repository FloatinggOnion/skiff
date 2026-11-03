package panel

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/nuelScript/skiff/internal/docker"
)

func testVolume(t *testing.T) string {
	t.Helper()
	if exec.Command("docker", "info").Run() != nil {
		t.Skip("docker not available")
	}
	name := fmt.Sprintf("skiff-restore-test-%d", time.Now().UnixNano())
	if out, err := exec.Command("docker", "volume", "create", name).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "volume", "rm", "-f", name).Run() })
	return name
}

func inVolume(t *testing.T, volume, script string) string {
	t.Helper()
	out, err := exec.Command("docker", "run", "--rm", "-v", volume+":/data", volumeToolImage, "sh", "-c", script).CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v: %s", script, err, out)
	}
	return strings.TrimSpace(string(out))
}

// failingMv makes mv fail for one file name, so the script's own undo has to run.
func failingMv(name string) string {
	return `mkdir -p /tmp/bin && printf '#!/bin/sh\ncase "$1" in *` + name + `*) exit 1;; esac\nexec /bin/busybox mv "$@"\n' > /tmp/bin/mv && chmod +x /tmp/bin/mv && export PATH=/tmp/bin:$PATH
`
}

const volumeListing = `cd /data && find . -mindepth 1 | sort | tr '\n' ' '; cat original .hidden`

func TestVolumeSwap(t *testing.T) {
	seed := `echo first > /data/original; echo dot > /data/.hidden; mkdir -p /data/sub /data/.restore-new/newdir; echo x > /data/sub/file; echo new > /data/.restore-new/restored; echo n > /data/.restore-new/newdir/f; echo d > /data/.restore-new/.newdot`
	originals := "./.hidden ./original ./sub ./sub/file first\ndot"
	eng := docker.Local()
	run := func(volume, prefix string) error {
		return eng.RunIO(context.Background(), volumeToolImage, []docker.Mount{{Name: volume, Destination: "/data"}}, prefix+volumeSwapScript, nil, &bytes.Buffer{})
	}

	t.Run("replaces the files", func(t *testing.T) {
		v := testVolume(t)
		inVolume(t, v, seed)
		if err := run(v, ""); err != nil {
			t.Fatal(err)
		}
		got := inVolume(t, v, `cd /data && find . -mindepth 1 | sort | tr '\n' ' '`)
		if want := "./.newdot ./newdir ./newdir/f ./restored"; got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("puts files back when the current ones can't move", func(t *testing.T) {
		v := testVolume(t)
		inVolume(t, v, seed+`; echo stuck > /data/boom`)
		err := run(v, failingMv("boom"))
		if err == nil || !strings.Contains(err.Error(), "current files aside") {
			t.Fatalf("want a clear failure, got %v", err)
		}
		got := inVolume(t, v, volumeListing)
		if !strings.Contains(got, "./boom") || !strings.Contains(got, "./original") || strings.Contains(got, ".restore") || !strings.HasSuffix(got, "first\ndot") {
			t.Fatalf("after failing: %q", got)
		}
	})

	t.Run("puts files back when the restored ones can't move", func(t *testing.T) {
		v := testVolume(t)
		inVolume(t, v, seed+`; echo stuck > /data/.restore-new/boom`)
		err := run(v, failingMv("boom"))
		if err == nil || !strings.Contains(err.Error(), "restored files in") {
			t.Fatalf("want a clear failure, got %v", err)
		}
		if got := inVolume(t, v, volumeListing); got != originals {
			t.Fatalf("after failing:\n got %q\nwant %q", got, originals)
		}
	})
}

func TestRedisLoadPutsDataBackOnFailure(t *testing.T) {
	v := testVolume(t)
	ctr := v + "-redis"
	image := dbEngines["redis"].image
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", ctr).Run() })
	if out, err := exec.Command("docker", "run", "-d", "--name", ctr, "-v", v+":/data", image, "redis-server", "--appendonly", "yes").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	cli := func(args ...string) (string, error) {
		out, err := exec.Command("docker", append([]string{"exec", ctr, "redis-cli"}, args...)...).CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	waitUp := func() {
		t.Helper()
		for range 100 {
			if out, err := cli("ping"); err == nil && out == "PONG" {
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
		t.Fatal("redis didn't start")
	}
	waitUp()
	if out, err := cli("set", "keep", "current"); err != nil || out != "OK" {
		t.Fatalf("%v: %s", err, out)
	}
	if out, err := exec.Command("docker", "stop", ctr).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	// Past redis-check-rdb, as a file that fools the checker would be: Redis itself refuses to load it.
	bad := strings.NewReader("REDIS0011 this is not a real dump")
	done := make(chan error, 1)
	go func() {
		done <- docker.Local().RunIO(context.Background(), image, []docker.Mount{{Name: v, Destination: "/data"}}, redisLoadScript, bad, &bytes.Buffer{})
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "load the backup") {
			t.Fatalf("want a clear failure, got %v", err)
		}
	case <-time.After(45 * time.Second):
		t.Fatal("a failed load should be noticed right away, not time out")
	}

	if out, err := exec.Command("docker", "start", ctr).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	waitUp()
	if got, _ := cli("get", "keep"); got != "current" {
		t.Fatalf("data after a failed load = %q, want current", got)
	}
	if got := inVolume(t, v, `ls -A /data | tr '\n' ' '`); strings.Contains(got, ".restore-old") {
		t.Fatalf("left behind: %q", got)
	}
}
