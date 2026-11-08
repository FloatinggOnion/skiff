package docker

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestNewVolumeBelongsToTheContainersUser checks that an image that doesn't run as root can write to a volume Skiff creates for it, and that root images and existing volumes are left alone.
func TestNewVolumeBelongsToTheContainersUser(t *testing.T) {
	if exec.Command("docker", "info").Run() != nil {
		t.Skip("docker not available")
	}
	e := Local()
	owner := func(volume string) string {
		out, err := exec.Command("docker", "run", "--rm", "-v", volume+":/v", VolumeTool, "stat", "-c", "%u:%g", "/v").CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	fresh := func() string {
		v := fmt.Sprintf("skiff-owner-test-%d", time.Now().UnixNano())
		t.Cleanup(func() { _ = exec.Command("docker", "volume", "rm", "-f", v).Run() })
		return v
	}

	for _, c := range []struct {
		name, user, want string
	}{
		{"a numeric user", "1000:1000", "1000:1000"},
		{"a named user, looked up in the image", "nobody", "65534:65534"},
		{"root", "", "0:0"},
	} {
		v := fresh()
		if err := e.EnsureAppVolumeFor(v, "app", "data", VolumeTool, c.user); err != nil {
			t.Fatal(err)
		}
		if got := owner(v); got != c.want {
			t.Errorf("%s: owner = %s, want %s", c.name, got, c.want)
		}
		if c.user != "" {
			if out, err := exec.Command("docker", "run", "--rm", "--user", c.user, "-v", v+":/v", VolumeTool, "touch", "/v/written").CombinedOutput(); err != nil {
				t.Errorf("%s can't write to its volume: %s", c.name, out)
			}
		}
	}

	// A volume that already exists keeps its owner: it may hold data someone else wrote.
	v := fresh()
	if err := e.EnsureAppVolume(v, "app", "data"); err != nil {
		t.Fatal(err)
	}
	if err := e.EnsureAppVolumeFor(v, "app", "data", VolumeTool, "1000"); err != nil {
		t.Fatal(err)
	}
	if got := owner(v); got != "0:0" {
		t.Errorf("an existing volume changed owner to %s", got)
	}
}
