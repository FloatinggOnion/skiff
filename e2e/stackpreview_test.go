//go:build e2e

package e2e

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nuelScript/skiff/internal/db"
)

const previewCompose = `services:
  web:
    build: ./api
    ports: ["3000"]
    depends_on: [db]
  db:
    image: postgres:16-alpine
    environment: {POSTGRES_USER: shop, POSTGRES_PASSWORD: "${DB_PASSWORD}", POSTGRES_DB: shop}
`

// TestStackPreviews follows a pull request through its life: opened, pushed to and closed, with signed webhooks as GitHub sends them.
func TestStackPreviews(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	bin := filepath.Join(root, "skiff")
	run(t, "", nil, "go", "build", "-o", bin, "..")
	repo, pushRepo := gitServer(t, root)
	work, bare := filepath.Join(root, "work"), filepath.Join(root, "srv", "site.git")
	gitEnv := []string{"GIT_AUTHOR_NAME=e2e", "GIT_AUTHOR_EMAIL=e2e@example.com", "GIT_COMMITTER_NAME=e2e", "GIT_COMMITTER_EMAIL=e2e@example.com"}
	write := func(path, content string) {
		t.Helper()
		full := filepath.Join(work, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dockerfile := func(page string) string {
		return "FROM busybox:1.36\nRUN mkdir /www && echo " + page + " > /www/index.html\nCMD [\"httpd\", \"-f\", \"-p\", \"3000\", \"-h\", \"/www\"]\n"
	}
	write("api/Dockerfile", dockerfile("page-main"))
	write("compose.yml", previewCompose)
	pushRepo("v1")
	branch := func(name, page string) {
		t.Helper()
		run(t, work, gitEnv, "git", "checkout", "-q", "-B", name, "main")
		write("api/Dockerfile", dockerfile(page))
		run(t, work, gitEnv, "git", "add", "-A")
		run(t, work, gitEnv, "git", "commit", "-q", "-m", name)
		run(t, work, gitEnv, "git", "push", "-q", "-f", bare, name)
		run(t, work, gitEnv, "git", "checkout", "-q", "main")
	}

	protectExisting(t, home)
	const hookSecret = "hook-secret"
	if err := os.WriteFile(filepath.Join(home, ".skiff", "github.json"), []byte(`{"id":1,"pem":"x","webhook_secret":"`+hookSecret+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	panel := freeAddr(t)
	start(t, []string{"HOME=" + home, "SKIFF_PANEL_PASSWORD=e2e-setup", "SKIFF_ALLOW_PRIVATE_ENDPOINTS=1"}, bin, "panel", "--addr", panel, "--domain", "localhost")
	base := "http://" + panel
	waitHTTP(t, base+"/api/me")
	c := client(t)
	post(t, c, base+"/api/auth/setup", `{"secret":"e2e-setup","email":"e2e@example.com","name":"E2E","password":"e2e-password-1"}`)

	const stack = "e2e-pv"
	t.Cleanup(func() {
		req, _ := http.NewRequest(http.MethodDelete, base+"/api/stacks?name="+stack, nil)
		if resp, err := c.Do(req); err == nil {
			resp.Body.Close()
		}
	})
	send := func(method, url, body string, want int) string {
		t.Helper()
		req, _ := http.NewRequest(method, base+url, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		msg, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != want {
			t.Fatalf("%s %s: %d %s", method, url, resp.StatusCode, msg)
		}
		return string(msg)
	}
	send(http.MethodPost, "/api/stacks", fmt.Sprintf(`{"name":%q,"git":%q,"vars":{"DB_PASSWORD":"pw-for-tests"}}`, stack, repo), http.StatusCreated)

	web := func(s string) string {
		out, _ := exec.Command("docker", "ps", "-q", "--filter", "label=skiff.app="+s+"-web").Output()
		return strings.TrimSpace(string(out))
	}
	dbOf := func(s string) string {
		out, _ := exec.Command("docker", "ps", "-q", "--filter", "label=skiff.stack="+s, "--filter", "label=skiff.kind=database").Output()
		return strings.TrimSpace(string(out))
	}
	page := func(s string) string {
		id := web(s)
		if id == "" {
			return ""
		}
		out, _ := exec.Command("docker", "exec", id, "cat", "/www/index.html").Output()
		return strings.TrimSpace(string(out))
	}
	sql := func(ctr, stmt string) string {
		t.Helper()
		return dockerExec(t, ctr, `psql -U shop -d shop -v ON_ERROR_STOP=1 -tAc "`+stmt+`"`)
	}
	eventually(t, 3*time.Minute, func() bool { return page(stack) == "page-main" })
	parentDB := dbOf(stack)
	sql(parentDB, "create table items(v text); insert into items values ('from-production')")

	// The test server's URL isn't github.com, so give the stack the name a webhook would carry.
	store, err := db.OpenAt(filepath.Join(home, ".skiff", "skiff.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Exec(`UPDATE stacks SET repo='acme/shop' WHERE name=?`, stack); err != nil {
		t.Fatal(err)
	}

	hook := func(event, body string) {
		t.Helper()
		mac := hmac.New(sha256.New, []byte(hookSecret))
		mac.Write([]byte(body))
		req, _ := http.NewRequest(http.MethodPost, base+"/api/github/hook", bytes.NewReader([]byte(body)))
		req.Header.Set("X-GitHub-Event", event)
		req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("webhook: %d", resp.StatusCode)
		}
	}
	pullRequest := func(action, name string) {
		t.Helper()
		hook("pull_request", fmt.Sprintf(`{"action":%q,"number":7,"pull_request":{"head":{"ref":%q,"repo":{"full_name":"acme/shop"}}},"repository":{"full_name":"acme/shop"}}`, action, name))
	}
	push := func(name string) {
		t.Helper()
		hook("push", fmt.Sprintf(`{"ref":"refs/heads/%s","after":"","repository":{"full_name":"acme/shop"},"head_commit":{"message":"change","added":[],"modified":["api/Dockerfile"],"removed":[]}}`, name))
	}
	type view struct {
		Name         string
		PreviewError string
		Previews     []struct{ Name, Branch string }
	}
	parent := func() view {
		t.Helper()
		var list []view
		getJSON(t, c, base+"/api/stacks", &list)
		if len(list) != 1 || list[0].Name != stack {
			t.Fatalf("a preview should sit under its stack, not beside it: %+v", list)
		}
		return list[0]
	}

	// Off by default: a pull request starts nothing.
	branch("feat/login", "page-branch")
	const preview = stack + "-feat-login"
	pullRequest("opened", "feat/login")
	time.Sleep(3 * time.Second)
	if got := parent().Previews; len(got) != 0 {
		t.Fatalf("a stack with previews off made one: %+v", got)
	}

	send(http.MethodPut, "/api/stacks/previews", fmt.Sprintf(`{"name":%q,"mode":"copy","password":"preview-secret"}`, stack), http.StatusNoContent)
	pullRequest("opened", "feat/login")
	eventually(t, 3*time.Minute, func() bool { return page(preview) == "page-branch" })
	if got := page(stack); got != "page-main" {
		t.Fatalf("the stack itself changed: %q", got)
	}
	if got := parent().Previews; len(got) != 1 || got[0].Name != preview || got[0].Branch != "feat/login" {
		t.Fatalf("previews = %+v", got)
	}

	// Its database is a separate copy, and the stack's own is out of reach from the preview.
	previewDB := dbOf(preview)
	if previewDB == "" || previewDB == parentDB {
		t.Fatalf("preview database %q, stack database %q", previewDB, parentDB)
	}
	eventually(t, time.Minute, func() bool {
		out, err := exec.Command("docker", "exec", previewDB, "psql", "-U", "shop", "-d", "shop", "-tAc", "select v from items").CombinedOutput()
		return err == nil && strings.TrimSpace(string(out)) == "from-production"
	})
	sql(previewDB, "insert into items values ('from-preview')")
	if got := sql(parentDB, "select count(*) from items"); got != "1" {
		t.Fatalf("a preview's write reached the stack: %s rows", got)
	}
	nameOf := func(id string) string {
		out, err := exec.Command("docker", "inspect", "-f", "{{.Name}}", id).Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimPrefix(strings.TrimSpace(string(out)), "/")
	}
	if got := dockerExec(t, web(preview), "nc -z -w 3 "+nameOf(parentDB)+" 5432 2>&1 && echo open || echo closed"); !strings.HasSuffix(got, "closed") {
		t.Fatalf("a preview reached the stack's database: %q", got)
	}
	if got := dockerExec(t, web(preview), "nc -z -w 3 db 5432 && echo open || echo closed"); got != "open" {
		t.Fatal("a preview couldn't reach its own db by service name")
	}

	// The preview asks for the password, and the stack itself doesn't.
	gated, err := os.ReadFile(filepath.Join(home, ".skiff", "protected.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gated), `"`+preview+`-web"`) || strings.Contains(string(gated), `"`+stack+`-web"`) {
		t.Fatalf("protected apps: %s", gated)
	}

	// A push to the branch reaches the preview and leaves the stack alone.
	branch("feat/login", "page-branch-two")
	push("feat/login")
	eventually(t, 3*time.Minute, func() bool { return page(preview) == "page-branch-two" })
	if got := page(stack); got != "page-main" {
		t.Fatalf("a push to a branch changed the stack: %q", got)
	}

	// Empty mode starts with no data.
	send(http.MethodPut, "/api/stacks/previews", fmt.Sprintf(`{"name":%q,"mode":"empty"}`, stack), http.StatusNoContent)
	branch("feat/empty", "page-empty")
	pullRequest("opened", "feat/empty")
	const emptyPreview = stack + "-feat-empty"
	eventually(t, 3*time.Minute, func() bool { return page(emptyPreview) == "page-empty" })
	if got := sql(dbOf(emptyPreview), "select coalesce(to_regclass('items')::text, 'none')"); got != "none" {
		t.Fatalf("an empty preview has the stack's table: %s", got)
	}

	// The limit holds, and says why.
	branch("feat/third", "page-third")
	send(http.MethodPost, "/api/stacks/previews", fmt.Sprintf(`{"name":%q,"branch":"feat/third"}`, stack), http.StatusOK)
	branch("feat/fourth", "page-fourth")
	if msg := send(http.MethodPost, "/api/stacks/previews", fmt.Sprintf(`{"name":%q,"branch":"feat/fourth"}`, stack), http.StatusConflict); !strings.Contains(msg, "limit") {
		t.Fatalf("over the limit: %s", msg)
	}

	// Closing the pull request takes everything away, and leaves the stack.
	previewNet := "skiff-s-" + preview
	pullRequest("closed", "feat/login")
	eventually(t, time.Minute, func() bool {
		out, _ := exec.Command("docker", "network", "ls", "-q", "--filter", "name="+previewNet).Output()
		return web(preview) == "" && dbOf(preview) == "" && strings.TrimSpace(string(out)) == ""
	})
	if got := sql(parentDB, "select v from items"); got != "from-production" {
		t.Fatalf("closing a pull request touched the stack's data: %q", got)
	}
	if got := page(stack); got != "page-main" {
		t.Fatalf("closing a pull request touched the stack: %q", got)
	}
	// Deleting the branch does the same.
	push2 := fmt.Sprintf(`{"ref":"refs/heads/feat/empty","after":"","deleted":true,"repository":{"full_name":"acme/shop"},"head_commit":null}`)
	hook("push", push2)
	eventually(t, time.Minute, func() bool { return web(emptyPreview) == "" && dbOf(emptyPreview) == "" })

	// A copy that can't be made removes the preview and says why, rather than starting with no data.
	send(http.MethodPut, "/api/stacks/previews", fmt.Sprintf(`{"name":%q,"mode":"copy"}`, stack), http.StatusNoContent)
	backups, _ := filepath.Glob(filepath.Join(home, ".skiff", "backups", "*", "*"))
	for _, f := range backups {
		_ = os.Remove(f)
	}
	branch("feat/broken", "page-broken")
	pullRequest("opened", "feat/broken")
	eventually(t, 2*time.Minute, func() bool { return parent().PreviewError != "" })
	if got := parent().PreviewError; !strings.Contains(got, "feat/broken") {
		t.Fatalf("preview error: %q", got)
	}
	if id := web(stack + "-feat-broken"); id != "" {
		t.Fatal("a preview started without the data it was meant to have")
	}

	// Deleting the stack takes the remaining preview with it.
	send(http.MethodDelete, "/api/stacks?name="+stack, "", http.StatusNoContent)
	eventually(t, time.Minute, func() bool { return web(stack+"-feat-third") == "" && dbOf(stack+"-feat-third") == "" })
}
