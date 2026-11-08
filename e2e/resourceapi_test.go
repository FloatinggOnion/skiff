//go:build e2e

package e2e

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestResourceAPI creates, attaches and removes a database, a bucket and a volume with API tokens.
func TestResourceAPI(t *testing.T) {
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
	base := "http://" + panel
	waitHTTP(t, base+"/api/me")
	c := client(t)
	post(t, c, base+"/api/auth/setup", `{"secret":"e2e-setup","email":"e2e@example.com","name":"E2E","password":"e2e-password-1"}`)

	var data, ci struct{ Token string }
	postJSON(t, c, base+"/api/tokens", `{"name":"laptop","databases":true}`, &data)
	postJSON(t, c, base+"/api/tokens", `{"name":"ci"}`, &ci)
	call := func(token, method, path, body string, into any) int {
		t.Helper()
		req, _ := http.NewRequest(method, base+"/api/v1"+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if into != nil {
			_ = json.NewDecoder(resp.Body).Decode(into)
		}
		return resp.StatusCode
	}

	const app = "e2e-res"
	t.Cleanup(func() {
		_ = exec.Command("sh", "-c", "docker rm -f $(docker ps -aq --filter label=skiff.app="+app+") ; docker rmi $(docker images -q skiff-"+app+")").Run()
	})
	var created struct{ App, ID string }
	postJSON(t, c, base+"/api/projects/image", `{"name":"`+app+`","image":"traefik/whoami:v1.10.2"}`, &created)
	stream(t, c, base+"/api/deploys/log?app="+app+"&id="+created.ID)
	env := func() map[string]string {
		t.Helper()
		var vars []struct{ Key, Value string }
		if code := call(data.Token, http.MethodGet, "/apps/"+app+"/env", "", &vars); code != http.StatusOK {
			t.Fatalf("reading env: %d", code)
		}
		out := map[string]string{}
		for _, v := range vars {
			out[v.Key] = v.Value
		}
		return out
	}

	type resource struct {
		Name     string   `json:"name"`
		State    string   `json:"state"`
		Attached []string `json:"attached"`
	}

	t.Run("database", func(t *testing.T) {
		var db resource
		if code := call(ci.Token, http.MethodPost, "/databases", `{"engine":"postgres","name":"api-pg"}`, &db); code != http.StatusCreated || db.Name != "api-pg" {
			t.Fatalf("creating a database: %d %+v", code, db)
		}
		t.Cleanup(func() { _ = call(data.Token, http.MethodDelete, "/databases/api-pg", "", nil) })
		if code := call(ci.Token, http.MethodPost, "/databases", `{"engine":"postgres","name":"api-pg"}`, nil); code != http.StatusConflict {
			t.Errorf("a second database with the same name: %d", code)
		}
		if code := call(ci.Token, http.MethodPost, "/databases", `{"engine":"oracle","name":"x"}`, nil); code != http.StatusBadRequest {
			t.Errorf("an unknown engine: %d", code)
		}
		if code := call(ci.Token, http.MethodPut, "/databases/api-pg/apps/"+app, "", nil); code != http.StatusForbidden {
			t.Errorf("a pipeline token attaching a database: %d", code)
		}
		if code := call(data.Token, http.MethodPut, "/databases/api-pg/apps/"+app, "", &db); code != http.StatusOK || !slices.Contains(db.Attached, app) {
			t.Fatalf("attaching: %d %+v", code, db)
		}
		var dbs []resource
		if code := call(ci.Token, http.MethodGet, "/databases", "", &dbs); code != http.StatusOK || len(dbs) != 1 || !slices.Contains(dbs[0].Attached, app) {
			t.Errorf("listing databases: %d %+v", code, dbs)
		}
		if !strings.HasPrefix(env()["DATABASE_URL"], "postgres") {
			t.Errorf("the app has no DATABASE_URL: %v", env())
		}
		if code := call(data.Token, http.MethodDelete, "/databases/api-pg/apps/"+app, "", &db); code != http.StatusOK || len(db.Attached) != 0 {
			t.Fatalf("detaching: %d %+v", code, db)
		}
		if _, ok := env()["DATABASE_URL"]; ok {
			t.Error("DATABASE_URL stayed after detaching")
		}
		if code := call(ci.Token, http.MethodDelete, "/databases/api-pg", "", nil); code != http.StatusForbidden {
			t.Errorf("a pipeline token deleting a database: %d", code)
		}
		if code := call(data.Token, http.MethodDelete, "/databases/api-pg", "", nil); code != http.StatusNoContent {
			t.Fatalf("deleting: %d", code)
		}
		if code := call(data.Token, http.MethodDelete, "/databases/api-pg", "", nil); code != http.StatusNotFound {
			t.Errorf("deleting it again: %d", code)
		}
	})

	t.Run("bucket", func(t *testing.T) {
		var b resource
		if code := call(ci.Token, http.MethodPost, "/buckets", `{"name":"api-files"}`, &b); code != http.StatusCreated || b.Name != "api-files" || b.State != "running" {
			t.Fatalf("creating a bucket: %d %+v", code, b)
		}
		t.Cleanup(func() { _ = call(data.Token, http.MethodDelete, "/buckets/api-files", "", nil) })
		var list []resource
		if code := call(ci.Token, http.MethodGet, "/buckets", "", &list); code != http.StatusOK || len(list) != 1 {
			t.Errorf("listing buckets: %d %+v", code, list)
		}
		if code := call(data.Token, http.MethodPut, "/buckets/api-files/apps/"+app, "", &b); code != http.StatusOK || !slices.Contains(b.Attached, app) {
			t.Fatalf("attaching: %d %+v", code, b)
		}
		if env()["S3_BUCKET"] != "api-files" {
			t.Errorf("the app has no S3_BUCKET: %v", env())
		}
		if code := call(ci.Token, http.MethodDelete, "/buckets/api-files", "", nil); code != http.StatusForbidden {
			t.Errorf("a pipeline token deleting a bucket: %d", code)
		}
		if code := call(data.Token, http.MethodDelete, "/buckets/api-files", "", nil); code != http.StatusNoContent {
			t.Fatalf("deleting: %d", code)
		}
		if _, ok := env()["S3_BUCKET"]; ok {
			t.Error("S3_BUCKET stayed after the bucket was deleted")
		}
	})

	t.Run("volume", func(t *testing.T) {
		if code := call(ci.Token, http.MethodPost, "/apps/"+app+"/volumes", `{"name":"data","path":"/data"}`, nil); code != http.StatusCreated {
			t.Fatalf("adding a volume: %d", code)
		}
		if code := call(ci.Token, http.MethodPost, "/apps/"+app+"/volumes", `{"name":"data","path":"/other"}`, nil); code != http.StatusBadRequest {
			t.Errorf("a second volume named data: %d", code)
		}
		var vols []struct{ Name, Path string }
		if code := call(ci.Token, http.MethodGet, "/apps/"+app+"/volumes", "", &vols); code != http.StatusOK || len(vols) != 1 || vols[0].Path != "/data" {
			t.Fatalf("listing volumes: %d %+v", code, vols)
		}
		if code := call(ci.Token, http.MethodDelete, "/apps/"+app+"/volumes/data", "", nil); code != http.StatusNoContent {
			t.Fatalf("removing the volume: %d", code)
		}
		if code := call(ci.Token, http.MethodDelete, "/apps/"+app+"/volumes/data", "", nil); code != http.StatusNotFound {
			t.Errorf("removing it again: %d", code)
		}
	})

	t.Run("another team sees none of it", func(t *testing.T) {
		var other struct{ ID string }
		postJSON(t, c, base+"/api/teams", `{"name":"other"}`, &other)
		var me struct{ Team string }
		getJSON(t, c, base+"/api/me", &me)
		post(t, c, base+"/api/auth/team", `{"team":"`+other.ID+`"}`)
		var theirs struct{ Token string }
		postJSON(t, c, base+"/api/tokens", `{"name":"other","databases":true}`, &theirs)
		post(t, c, base+"/api/auth/team", `{"team":"`+me.Team+`"}`)
		if code := call(theirs.Token, http.MethodGet, "/apps/"+app+"/volumes", "", nil); code != http.StatusNotFound {
			t.Errorf("another team listing volumes: %d", code)
		}
		if code := call(theirs.Token, http.MethodPost, "/apps/"+app+"/volumes", `{"name":"x","path":"/x"}`, nil); code != http.StatusNotFound {
			t.Errorf("another team adding a volume: %d", code)
		}
	})

	t.Run("cli", func(t *testing.T) {
		creds, _ := json.Marshal(map[string]string{"url": base, "token": data.Token})
		if err := os.WriteFile(filepath.Join(home, ".skiff", "credentials.json"), creds, 0o600); err != nil {
			t.Fatal(err)
		}
		skiff := func(args ...string) (string, error) {
			cmd := exec.Command(bin, args...)
			cmd.Env = append(os.Environ(), "HOME="+home)
			out, err := cmd.CombinedOutput()
			return string(out), err
		}
		if out, err := skiff("volume", "add", app, "cache", "/cache"); err != nil {
			t.Fatalf("volume add: %v %s", err, out)
		}
		if out, err := skiff("volume", "ls", app); err != nil || !strings.Contains(out, "/cache") {
			t.Fatalf("volume ls: %v %s", err, out)
		}
		if out, err := skiff("volume", "rm", app, "cache"); err == nil || !strings.Contains(out, "--yes") {
			t.Errorf("volume rm without --yes: %v %s", err, out)
		}
		if out, err := skiff("volume", "rm", app, "cache", "--yes"); err != nil {
			t.Fatalf("volume rm: %v %s", err, out)
		}
		if out, err := skiff("db", "attach", "missing", app); err == nil || !strings.Contains(out, "database not found") {
			t.Errorf("attaching a missing database: %v %s", err, out)
		}
	})

	t.Run("mcp", func(t *testing.T) {
		tools := func(readOnly bool) (map[string]bool, func(string, map[string]any) (string, bool)) {
			args := []string{"mcp"}
			if readOnly {
				args = append(args, "--read-only")
			}
			cmd := exec.Command(bin, args...)
			cmd.Env = append(os.Environ(), "SKIFF_URL="+base, "SKIFF_TOKEN="+ci.Token)
			stdin, _ := cmd.StdinPipe()
			stdout, _ := cmd.StdoutPipe()
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = stdin.Close(); _ = cmd.Wait() })
			out := bufio.NewScanner(stdout)
			out.Buffer(make([]byte, 0, 64*1024), 8<<20)
			id := 0
			rpc := func(method string, params any) map[string]any {
				id++
				b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
				_, _ = stdin.Write(append(b, '\n'))
				if !out.Scan() {
					t.Fatalf("mcp closed during %s", method)
				}
				var r map[string]any
				_ = json.Unmarshal(out.Bytes(), &r)
				r, _ = r["result"].(map[string]any)
				return r
			}
			rpc("initialize", map[string]any{"protocolVersion": "2025-06-18"})
			names := map[string]bool{}
			for _, tl := range rpc("tools/list", nil)["tools"].([]any) {
				names[tl.(map[string]any)["name"].(string)] = true
			}
			return names, func(name string, args map[string]any) (string, bool) {
				r := rpc("tools/call", map[string]any{"name": name, "arguments": args})
				return r["content"].([]any)[0].(map[string]any)["text"].(string), r["isError"].(bool)
			}
		}

		ro, _ := tools(true)
		if !ro["list_databases"] || !ro["list_volumes"] || ro["create_database"] || ro["add_volume"] || ro["set_worker"] {
			t.Errorf("read-only tools: %v", ro)
		}
		all, tool := tools(false)
		for name := range all {
			if strings.HasPrefix(name, "delete_") || name == "remove_volume" {
				t.Errorf("an agent can delete data with %s", name)
			}
		}
		if text, isErr := tool("add_volume", map[string]any{"name": app, "volume": "agent", "path": "/agent"}); isErr {
			t.Fatalf("add_volume: %s", text)
		}
		if text, isErr := tool("list_volumes", map[string]any{"name": app}); isErr || !strings.Contains(text, "/agent") {
			t.Fatalf("list_volumes: %s", text)
		}
		if text, isErr := tool("attach_database", map[string]any{"database": "missing", "name": app}); !isErr || !strings.Contains(text, "can't reach") {
			t.Errorf("a pipeline token attaching through mcp: %s", text)
		}
	})

	var activity []struct{ Action, Detail string }
	getJSON(t, c, base+"/api/audit", &activity)
	seen := map[string]bool{}
	for _, a := range activity {
		if strings.HasSuffix(a.Detail, "via api") {
			seen[a.Action] = true
		}
	}
	for _, want := range []string{"database.create", "database.attach", "database.detach", "database.delete", "bucket.create", "bucket.attach", "bucket.delete", "volume.add", "volume.remove"} {
		if !seen[want] {
			t.Errorf("Activity is missing %s", want)
		}
	}
	post(t, c, base+"/api/down?app="+app, "")
}
