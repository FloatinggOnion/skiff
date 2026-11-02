//go:build e2e

package e2e

import (
	"bytes"
	"crypto/rand"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestObjectStorage provisions a bucket through the dashboard and uses it from another container on the team network.
func TestObjectStorage(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	bin := filepath.Join(root, "skiff")
	run(t, "", nil, "go", "build", "-o", bin, "..")
	protectExisting(t, home)

	panel := freeAddr(t)
	start(t, []string{"HOME=" + home, "SKIFF_PANEL_PASSWORD=e2e-setup"}, bin, "panel", "--addr", panel, "--domain", "localhost")
	waitHTTP(t, "http://"+panel+"/api/me")
	c := client(t)
	post(t, c, "http://"+panel+"/api/auth/setup", `{"secret":"e2e-setup","email":"e2e@example.com","name":"E2E","password":"e2e-password-1"}`)

	var b struct {
		ID, Name, Endpoint, AccessKey, SecretKey, State string
	}
	postJSON(t, c, "http://"+panel+"/api/storage", `{"name":"media"}`, &b)
	ctr := strings.TrimSuffix(strings.TrimPrefix(b.Endpoint, "http://"), ":8333")
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", ctr).Run()
		_ = exec.Command("docker", "volume", "rm", ctr+"-data").Run()
	})
	if b.Name != "media" || b.State != "running" {
		t.Fatalf("bucket = %+v", b)
	}
	netOut, err := exec.Command("docker", "inspect", ctr, "--format", "{{range $k, $v := .NetworkSettings.Networks}}{{$k}} {{end}}").Output()
	if err != nil {
		t.Fatal(err)
	}
	network := strings.Fields(string(netOut))[0]

	run(t, "", nil, "docker", "pull", "-q", "curlimages/curl:8.10.1")
	curl := func(user string, args ...string) string {
		t.Helper()
		full := append([]string{"run", "--rm", "--network", network, "curlimages/curl:8.10.1", "-s", "-o", "/dev/null", "-w", "%{http_code}",
			"--aws-sigv4", "aws:amz:us-east-1:s3", "--user", user}, args...)
		out, err := exec.Command("docker", full...).Output()
		if err != nil {
			t.Fatalf("curl: %v %s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	creds := b.AccessKey + ":" + b.SecretKey
	if code := curl(creds, "-X", "PUT", "--data-binary", "hello", b.Endpoint+"/media/hello.txt"); code != "200" {
		t.Fatalf("put = %s", code)
	}
	if code := curl(creds, b.Endpoint+"/media/hello.txt"); code != "200" {
		t.Fatalf("get = %s", code)
	}
	if code := curl(b.AccessKey+":wrong", b.Endpoint+"/media/hello.txt"); code != "403" {
		t.Fatalf("a wrong key got %s", code)
	}

	checkBucketBrowser(t, c, panel, b.ID)
}

// checkBucketBrowser uses the panel's file endpoints against the real store: nested and oddly named keys, binary content, the object an app wrote, and delete.
func checkBucketBrowser(t *testing.T, c *http.Client, panel, id string) {
	t.Helper()
	objectURL := func(key string) string {
		return "http://" + panel + "/api/storage/object?id=" + id + "&key=" + url.QueryEscape(key)
	}
	type listing struct {
		Objects []struct {
			Key      string `json:"key"`
			Size     int64  `json:"size"`
			Modified int64  `json:"modified"`
		} `json:"objects"`
		Folders []string `json:"folders"`
		Next    string   `json:"next"`
	}
	list := func(prefix, token string) listing {
		t.Helper()
		var l listing
		getJSON(t, c, "http://"+panel+"/api/storage/objects?id="+id+"&prefix="+url.QueryEscape(prefix)+"&token="+url.QueryEscape(token), &l)
		return l
	}
	do := func(method, key string, body []byte) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(method, objectURL(key), bytes.NewReader(body))
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	// The file the app wrote directly is visible.
	root := list("", "")
	if len(root.Objects) != 1 || root.Objects[0].Key != "hello.txt" || root.Objects[0].Size != 5 || root.Objects[0].Modified == 0 {
		t.Fatalf("root = %+v", root)
	}

	payload := make([]byte, 3<<20)
	_, _ = rand.Read(payload)
	const nested = "docs/2026 report+final é.bin"
	resp := do(http.MethodPut, nested, payload)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("upload: %d", resp.StatusCode)
	}
	root = list("", "")
	if len(root.Folders) != 1 || root.Folders[0] != "docs/" {
		t.Fatalf("folders = %+v", root)
	}
	docs := list("docs/", "")
	if len(docs.Objects) != 1 || docs.Objects[0].Key != nested || docs.Objects[0].Size != int64(len(payload)) {
		t.Fatalf("docs = %+v", docs)
	}

	resp = do(http.MethodGet, nested, nil)
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.Equal(got, payload) {
		t.Fatalf("download differs: %d bytes, want %d", len(got), len(payload))
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") {
		t.Fatalf("Content-Disposition = %q", cd)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/octet-stream" {
		t.Fatalf("Content-Type = %q", ct)
	}

	resp = do(http.MethodPut, "../escape.txt", []byte("x"))
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("a path out of the bucket: %d", resp.StatusCode)
	}
	resp = do(http.MethodGet, "docs/missing.txt", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing file: %d", resp.StatusCode)
	}

	// Paging walks everything without repeats.
	for i := range 205 {
		resp := do(http.MethodPut, "many/f"+strings.Repeat("0", 3-len(itoa(i)))+itoa(i), []byte("x"))
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("upload %d: %d", i, resp.StatusCode)
		}
	}
	first := list("many/", "")
	if len(first.Objects) != 200 || first.Next == "" {
		t.Fatalf("first page: %d objects, next %q", len(first.Objects), first.Next)
	}
	second := list("many/", first.Next)
	if len(second.Objects) != 5 || second.Next != "" || second.Objects[0].Key == first.Objects[199].Key {
		t.Fatalf("second page: %d objects, next %q", len(second.Objects), second.Next)
	}

	resp = do(http.MethodDelete, nested, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	if docs := list("docs/", ""); len(docs.Objects) != 0 {
		t.Fatalf("after delete: %+v", docs)
	}
}

func itoa(i int) string { return strconv.Itoa(i) }
