package registry

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRemote runs the real remote script under a throwaway $HOME instead of over ssh.
func fakeRemote(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	orig := sshCommand
	sshCommand = func(_, script string) *exec.Cmd {
		c := exec.Command("sh", "-c", script)
		c.Env = append(os.Environ(), "HOME="+home)
		return c
	}
	t.Cleanup(func() { sshCommand = orig })
	return home
}

func readApps(t *testing.T, home string) map[string]App {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, ".skiff", "apps.json"))
	if err != nil {
		t.Fatal(err)
	}
	apps := map[string]App{}
	if err := json.Unmarshal(data, &apps); err != nil {
		t.Fatalf("remote registry is not valid JSON: %v\n%s", err, data)
	}
	return apps
}

func TestPutRemoteRegistersAppAsLocalToTheServer(t *testing.T) {
	home := fakeRemote(t)
	err := PutRemote("root@example", App{
		Name: "portfolio-v1", Container: "portfolio-v1-842dff", Port: 3000, HostPort: 32789,
		Host:     "root@example",
		Replicas: []Replica{{Container: "portfolio-v1-842dff", HostPort: 32789}},
	})
	if err != nil {
		t.Fatalf("PutRemote: %v", err)
	}
	got := readApps(t, home)["portfolio-v1"]
	if got.Container != "portfolio-v1-842dff" || got.HostPort != 32789 {
		t.Errorf("entry not written: %+v", got)
	}
	if got.Host != "" {
		t.Errorf("server-side entry must be local to the server, got Host=%q", got.Host)
	}
}

func TestPutRemotePreservesOtherApps(t *testing.T) {
	home := fakeRemote(t)
	for _, name := range []string{"www", "tenny-book", "hgm-cards"} {
		if err := PutRemote("h", App{Name: name, Container: name + "-c"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := DeleteRemote("h", "tenny-book"); err != nil {
		t.Fatal(err)
	}
	apps := readApps(t, home)
	if len(apps) != 2 || apps["www"].Container != "www-c" || apps["hgm-cards"].Container != "hgm-cards-c" {
		t.Errorf("unexpected remote registry: %+v", apps)
	}
}

func TestUpdateRemoteNeverClobbersACorruptRegistry(t *testing.T) {
	home := fakeRemote(t)
	path := filepath.Join(home, ".skiff", "apps.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	corrupt := []byte(`{"www": {"name": "www", "container": `)
	if err := os.WriteFile(path, corrupt, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := PutRemote("h", App{Name: "x", Container: "x-c"}); err == nil {
		t.Fatal("expected an error for a corrupt remote registry")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(corrupt) {
		t.Errorf("remote registry was rewritten after an aborted update:\n%s", after)
	}
}

func TestUpdateRemoteReportsAnUnreachableServer(t *testing.T) {
	orig := sshCommand
	sshCommand = func(_, _ string) *exec.Cmd {
		return exec.Command("sh", "-c", "echo 'ssh: connect to host example port 22: Connection refused' >&2; exit 255")
	}
	t.Cleanup(func() { sshCommand = orig })

	err := PutRemote("root@example", App{Name: "x"})
	if err == nil || !strings.Contains(err.Error(), "Connection refused") {
		t.Errorf("want an error surfacing ssh's message, got %v", err)
	}
}

func TestSSHTargetCannotBeAnOption(t *testing.T) {
	args := sshCommand("-oProxyCommand=id", "true").Args
	for i, a := range args {
		if a == "-oProxyCommand=id" {
			if args[i-1] != "--" {
				t.Fatalf("target must follow --: %v", args)
			}
			return
		}
	}
	t.Fatalf("target missing: %v", args)
}
