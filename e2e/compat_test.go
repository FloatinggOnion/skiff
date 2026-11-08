//go:build e2e

package e2e

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nuelScript/skiff/internal/compose"
)

// compatProject is a real project's own Compose file, pinned to a commit so the score only moves when Skiff does.
type compatProject struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Repo   string `json:"repo"`
	Commit string `json:"commit"`
	File   string `json:"file"`
}

type compatPlanResult struct {
	compatProject
	Accepted bool `json:"accepted"`
	// Unreachable means the project couldn't be downloaded, which says nothing about Skiff.
	Unreachable string   `json:"unreachable,omitempty"`
	Problems    []string `json:"problems,omitempty"`
	Ignored     []string `json:"ignored,omitempty"`
	Notes       []string `json:"notes,omitempty"`
	Services    int      `json:"services"`

	plan     *compose.Plan
	env      map[string]string
	envFiles []string
}

func compatProjects(t *testing.T) []compatProject {
	t.Helper()
	data, err := os.ReadFile("testdata/compat/projects.json")
	if err != nil {
		t.Fatal(err)
	}
	var list []compatProject
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatal(err)
	}
	return list
}

var (
	compatIgnored  = regexp.MustCompile(`^[^:]+: (\S+) is ignored$`)
	compatRequired = regexp.MustCompile(`^(\w+) must be set`)
)

// compatHarmless are settings Skiff ignores without changing how the software behaves.
var compatHarmless = []string{"attach", "container_name", "hostname", "labels", "logging", "networks", "pull_policy", "restart", "stdin_open", "tty"}

// compatEnvFiles are the names projects give the file a user copies to .env.
var compatEnvFiles = []string{".env.example", ".env.sample", ".env.template", ".env.dist", "example.env", "env.example", "sample.env", ".env"}

// compatEnv reads the project's own example values, as someone following its instructions would.
func compatEnv(work string, p compatProject) map[string]string {
	for _, dir := range []string{filepath.Join(work, filepath.Dir(p.File)), work} {
		for _, name := range compatEnvFiles {
			if data, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
				return compose.ParseEnvFile(data)
			}
		}
	}
	return map[string]string{}
}

// compatEnvExample finds the example a user would copy to a missing env file: the same name with an example suffix, or the folder's usual example.
func compatEnvExample(path string) string {
	dir, base := filepath.Split(path)
	var names []string
	for _, suffix := range []string{".example", ".sample", ".template", ".dist"} {
		names = append(names, base+suffix)
	}
	for _, name := range append(names, compatEnvFiles...) {
		if full := filepath.Join(dir, name); full != path {
			if info, err := os.Stat(full); err == nil && info.Mode().IsRegular() {
				return full
			}
		}
	}
	return ""
}

// compatFill gives a value to anything the file insists on that the example doesn't provide.
func compatFill(name string) string {
	if strings.Contains(strings.ToUpper(name), "PORT") {
		return "18080"
	}
	return "compat-value"
}

func compatParse(data []byte, env map[string]string) (*compose.File, error) {
	lookup := func(k string) (string, bool) {
		v, ok := env[k]
		return v, ok && v != ""
	}
	for {
		f, err := compose.Parse(data, lookup)
		if err != nil {
			m := compatRequired.FindStringSubmatch(err.Error())
			if m == nil || env[m[1]] != "" {
				return nil, err
			}
			env[m[1]] = compatFill(m[1])
			continue
		}
		missing := false
		for _, k := range f.Unset {
			if env[k] == "" {
				env[k] = compatFill(k)
				missing = true
			}
		}
		if !missing {
			return f, nil
		}
	}
}

// compatLoad checks the project out at its pinned commit and plans it the way the panel would.
func compatLoad(p compatProject, work string) compatPlanResult {
	r := compatPlanResult{compatProject: p}
	if err := compatCheckout(p, work); err != nil {
		r.Unreachable = err.Error()
		return r
	}
	dir := filepath.Join(work, filepath.Dir(p.File))
	data, err := os.ReadFile(filepath.Join(work, p.File))
	if err != nil {
		r.Unreachable = err.Error()
		return r
	}
	env := compatEnv(work, p)
	f, err := compatParse(data, env)
	if err != nil {
		r.Problems = []string{err.Error()}
		return r
	}
	plan := compose.BuildPlan("compat", f, func(file string) ([]byte, error) {
		body, err := os.ReadFile(filepath.Join(dir, file))
		if err == nil {
			return body, nil
		}
		// A missing .env is the one the user would have created from the example.
		if example := compatEnvExample(filepath.Join(dir, file)); example != "" {
			return os.ReadFile(example)
		}
		return nil, err
	}, func(path string) compose.PathKind {
		switch info, err := os.Stat(filepath.Join(dir, path)); {
		case err != nil:
			return compose.PathMissing
		case info.IsDir():
			return compose.PathDir
		}
		return compose.PathFile
	})
	r.plan, r.env = plan, env
	for _, s := range f.Services {
		r.envFiles = append(r.envFiles, s.EnvFiles...)
	}
	r.Services = len(f.Services)
	r.Problems = plan.Problems
	for _, n := range plan.Notes {
		if m := compatIgnored.FindStringSubmatch(n); m != nil {
			if !slices.Contains(compatHarmless, m[1]) {
				r.Ignored = append(r.Ignored, m[1])
			}
		} else {
			r.Notes = append(r.Notes, n)
		}
	}
	slices.Sort(r.Ignored)
	r.Ignored = slices.Compact(r.Ignored)
	r.Accepted = len(r.Problems) == 0
	return r
}

// TestCompatPlan reads every project's file with Skiff's own planner. The result is a score, so a refusal doesn't fail the test.
func TestCompatPlan(t *testing.T) {
	if os.Getenv("SKIFF_COMPAT") == "" {
		t.Skip("set SKIFF_COMPAT=1: this downloads every project's Compose file")
	}
	list := compatProjects(t)
	results := make([]compatPlanResult, len(list))
	var wg sync.WaitGroup
	slots := make(chan struct{}, 8)
	for i, p := range list {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			work := filepath.Join(t.TempDir(), p.Name)
			results[i] = compatLoad(p, work)
			_ = os.RemoveAll(work)
		}()
	}
	wg.Wait()
	for _, r := range results {
		if r.Unreachable != "" {
			t.Errorf("%s couldn't be read, so this score means nothing: %s", r.Name, r.Unreachable)
		}
	}
	compatWrite(t, "plan", results)
}

// compatRunResult is how far a project got on a real panel.
type compatRunResult struct {
	compatProject
	// Outcome is refused, failed, crashed, silent or runs, or unmeasured when the test itself couldn't set up.
	Outcome string `json:"outcome"`
	// Control is why plain Docker Compose couldn't run the same file with the same example settings. Those projects are broken or need setup steps first, so they don't count toward the score.
	Control string `json:"control,omitempty"`
	Detail  string `json:"detail,omitempty"`
	Seconds int    `json:"seconds,omitempty"`
}

// TestCompatRun deploys what the planner accepts and checks it stays up, answers, and survives a restart and a redeploy. SKIFF_COMPAT_KIND or SKIFF_COMPAT_ONLY narrow the list.
func TestCompatRun(t *testing.T) {
	if os.Getenv("SKIFF_COMPAT") == "" {
		t.Skip("set SKIFF_COMPAT=1: this pulls and runs real projects")
	}
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

	backend, err := exec.Command("git", "--exec-path").Output()
	if err != nil {
		t.Fatal(err)
	}
	gitRoot := filepath.Join(root, "srv")
	if err := os.MkdirAll(gitRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(&cgi.Handler{
		Path: filepath.Join(strings.TrimSpace(string(backend)), "git-http-backend"),
		Env:  []string{"GIT_PROJECT_ROOT=" + gitRoot, "GIT_HTTP_EXPORT_ALL=1"},
	})
	defer srv.Close()

	kind, only := os.Getenv("SKIFF_COMPAT_KIND"), strings.Split(os.Getenv("SKIFF_COMPAT_ONLY"), ",")
	// Not a test temp folder: containers can leave files in here that only root can delete.
	scratch, err := os.MkdirTemp("", "skiff-compat-")
	if err != nil {
		t.Fatal(err)
	}
	var results []compatRunResult
	for _, p := range compatProjects(t) {
		if kind != "" && p.Kind != kind || only[0] != "" && !slices.Contains(only, p.Name) {
			continue
		}
		out := compatRunResult{compatProject: p}
		work := filepath.Join(scratch, p.Name)
		r := compatLoad(p, work)
		if r.Unreachable != "" {
			// This says nothing about Skiff, so it can't count toward the score.
			t.Errorf("%s: couldn't check it out: %s", p.Name, r.Unreachable)
			out.Outcome, out.Detail = "unmeasured", r.Unreachable
		} else if err := compatPublish(gitRoot, work, r); err != nil {
			t.Errorf("%s: couldn't prepare the repository: %v", p.Name, err)
			out.Outcome, out.Detail = "unmeasured", err.Error()
		} else {
			out.Control = compatControl(work, r)
			if !r.Accepted {
				out.Outcome, out.Detail = "refused", strings.Join(r.Problems, "; ")
			} else {
				began := time.Now()
				out.Outcome, out.Detail = compatDeploy(t, c, "http://"+panel, srv.URL, r)
				out.Seconds = int(time.Since(began).Seconds())
			}
		}
		_ = os.RemoveAll(work)
		t.Logf("%s: %s %s", p.Name, out.Outcome, out.Detail)
		results = append(results, out)
		compatWrite(t, "run-"+cmp.Or(kind, "all"), results)
		compatSummary(t, results)
		if os.Getenv("SKIFF_COMPAT_PRUNE") != "" {
			_ = exec.Command("docker", "system", "prune", "-af", "--volumes").Run()
		}
	}
}

func compatDeploy(t *testing.T, c *http.Client, panel, gitURL string, r compatPlanResult) (outcome, detail string) {
	t.Helper()
	stack := "c-" + r.Name
	if len(stack) > 20 {
		stack = strings.TrimRight(stack[:20], "-")
	}
	base := panel + "/api/stacks"
	defer func() {
		req, _ := http.NewRequest(http.MethodDelete, base+"?name="+stack, nil)
		if resp, err := c.Do(req); err == nil {
			resp.Body.Close()
		}
	}()
	body, _ := json.Marshal(map[string]any{"name": stack, "git": gitURL + "/" + r.Name + ".git", "path": r.File, "vars": r.env})
	resp, err := c.Post(base, "application/json", bytes.NewReader(body))
	if err != nil {
		return "failed", err.Error()
	}
	msg, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return "refused", fmt.Sprintf("%d %s", resp.StatusCode, msg)
	}

	members := func() []stackMember {
		var list []struct {
			Name    string
			Members []stackMember
		}
		if err := compatGet(c, base, &list); err != nil {
			return nil
		}
		for _, s := range list {
			if s.Name == stack {
				return s.Members
			}
		}
		return nil
	}
	waiting := func() string {
		m := members()
		if len(m) == 0 {
			return "no services yet"
		}
		var pending []string
		for _, x := range m {
			if x.State != "running" {
				pending = append(pending, x.App+" is "+x.State)
			}
		}
		return strings.Join(pending, ", ")
	}
	var left string
	var looping time.Time
	for deadline := time.Now().Add(12 * time.Minute); time.Now().Before(deadline); time.Sleep(5 * time.Second) {
		if left = waiting(); left == "" {
			break
		}
		// A database that keeps restarting won't recover, so don't wait out the limit.
		if !strings.Contains(left, "restarting") {
			looping = time.Time{}
		} else if looping.IsZero() {
			looping = time.Now()
		} else if time.Since(looping) > 4*time.Minute {
			break
		}
	}
	if left != "" {
		return "failed", left + ": " + compatWhy(c, panel, stack, members())
	}
	ids := func() []string {
		out, _ := exec.Command("docker", "ps", "-aq", "--filter", "label=skiff.stack="+stack).Output()
		var apps []string
		for _, m := range members() {
			got, _ := exec.Command("docker", "ps", "-aq", "--filter", "label=skiff.app="+m.App).Output()
			apps = append(apps, strings.Fields(string(got))...)
		}
		return append(strings.Fields(string(out)), apps...)
	}
	time.Sleep(45 * time.Second)
	for _, id := range ids() {
		out, _ := exec.Command("docker", "inspect", "-f", "{{.State.Status}} {{.RestartCount}} {{.Name}}", id).Output()
		if f := strings.Fields(string(out)); len(f) == 3 && (f[0] != "running" || f[1] != "0") {
			return "crashed", f[2] + " is " + f[0] + " after " + f[1] + " restarts: " + compatLogTail(id)
		}
	}
	appOf := func(service string) string {
		for _, m := range members() {
			if m.Service == service {
				return m.App
			}
		}
		return ""
	}
	if why := compatAnswers(r, appOf); why != "" {
		return "silent", why
	}
	for _, id := range ids() {
		_ = exec.Command("docker", "restart", id).Run()
	}
	if why := compatAnswers(r, appOf); why != "" {
		return "crashed", "after a restart: " + why
	}

	// A redeploy replaces every app's containers, which is where shared volumes and start order get tested again.
	for _, m := range members() {
		if m.Kind != "app" {
			continue
		}
		if resp, err := c.Post(panel+"/api/redeploy?app="+m.App, "application/json", nil); err == nil {
			resp.Body.Close()
		}
	}
	time.Sleep(10 * time.Second)
	left = waiting()
	for deadline := time.Now().Add(12 * time.Minute); left != "" && time.Now().Before(deadline); time.Sleep(5 * time.Second) {
		left = waiting()
	}
	if left != "" {
		return "crashed", "after a redeploy: " + left + ": " + compatWhy(c, panel, stack, members())
	}
	if why := compatAnswers(r, appOf); why != "" {
		return "crashed", "after a redeploy: " + why
	}
	return "runs", ""
}

func compatGet(c *http.Client, url string, into any) error {
	resp, err := c.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(into)
}

// compatLogTail prefers the last line that reads like an error, since the final lines are often noise.
func compatLogTail(id string) string {
	out, _ := exec.Command("docker", "logs", "--tail", "40", id).CombinedOutput()
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	s := strings.Join(lines[max(0, len(lines)-3):], " ")
	for _, l := range slices.Backward(lines) {
		if low := strings.ToLower(l); strings.Contains(low, "error") || strings.Contains(low, "fatal") || strings.Contains(low, "panic") {
			s = l
			break
		}
	}
	s = strings.Join(strings.Fields(s), " ")
	return s[:min(len(s), 400)]
}

// compatWhy finds the reason something isn't running: its own last words, or the end of its deploy log.
func compatWhy(c *http.Client, panel, stack string, members []stackMember) string {
	var why []string
	out, _ := exec.Command("docker", "ps", "-a", "--filter", "label=skiff.stack="+stack, "--format", "{{.ID}} {{.Names}} {{.State}}").Output()
	for line := range strings.Lines(string(out)) {
		if f := strings.Fields(line); len(f) == 3 && f[2] != "running" {
			why = append(why, f[1]+": "+compatLogTail(f[0]))
		}
	}
	for _, m := range members {
		if m.State == "running" || m.Kind == "database" {
			continue
		}
		var deploys []struct{ ID string }
		if compatGet(c, panel+"/api/deploys?app="+m.App, &deploys) != nil || len(deploys) == 0 {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, panel+"/api/deploys/log?app="+m.App+"&id="+deploys[0].ID, nil)
		if resp, err := c.Do(req); err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			var errs []string
			for line := range strings.Lines(string(body)) {
				if low := strings.ToLower(line); strings.Contains(low, "error") || strings.Contains(low, "fatal") || strings.Contains(low, "failed to") {
					errs = append(errs, strings.Join(strings.Fields(line), " "))
				}
			}
			text := strings.Join(strings.Fields(string(body)), " ")
			detail := text[max(0, len(text)-300):]
			if len(errs) > 0 {
				first := strings.Join(errs[:min(len(errs), 3)], " / ")
				detail = first[:min(len(first), 600)] + " … " + detail
			}
			why = append(why, m.App+": "+detail)
		}
		cancel()
		break
	}
	return strings.Join(why, " | ")
}

// compatControl runs the same file under plain Docker Compose, and returns why it couldn't.
func compatControl(work string, r compatPlanResult) string {
	project := "compat-" + r.Name
	env := os.Environ()
	for k, v := range r.env {
		env = append(env, k+"="+v)
	}
	compose := func(limit time.Duration, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), limit)
		defer cancel()
		cmd := exec.CommandContext(ctx, "docker", append([]string{"compose", "-p", project, "-f", r.File}, args...)...)
		cmd.Dir, cmd.Env = work, env
		return cmd.CombinedOutput()
	}
	defer func() { _, _ = compose(5*time.Minute, "down", "-v", "--remove-orphans", "--rmi", "local") }()
	if out, err := compose(15*time.Minute, "up", "-d", "--build", "--quiet-pull"); err != nil {
		text := strings.Join(strings.Fields(string(out)), " ")
		return "compose up: " + text[max(0, len(text)-300):]
	}
	time.Sleep(60 * time.Second)
	out, err := compose(time.Minute, "ps", "-a", "--format", "{{.Name}} {{.State}} {{.ExitCode}}")
	if err != nil {
		return "compose ps: " + err.Error()
	}
	var bad []string
	for line := range strings.Lines(string(out)) {
		f := strings.Fields(line)
		if len(f) == 3 && (f[1] == "restarting" || f[1] == "dead" || f[1] == "exited" && f[2] != "0") {
			bad = append(bad, f[0]+" "+f[1])
		}
	}
	return strings.Join(bad, ", ")
}

// compatSummary writes the score. Only projects plain Compose can run count toward it.
func compatSummary(t *testing.T, results []compatRunResult) {
	t.Helper()
	count := map[string]int{}
	broken := 0
	var beyond []string
	for _, r := range results {
		if r.Control != "" {
			broken++
			if r.Outcome == "runs" {
				beyond = append(beyond, r.Name)
			}
			continue
		}
		count[r.Outcome]++
		count["total"]++
	}
	var b strings.Builder
	fmt.Fprintf(&b, "### Runs on Skiff: %d of %d\n\n", count["runs"], count["total"])
	fmt.Fprintf(&b, "%d crashed, %d silent, %d failed to deploy, %d refused, %d unmeasured. %d more don't run under plain Compose with their example settings: some are broken, others need setup steps first.\n\n", count["crashed"], count["silent"], count["failed"], count["refused"], count["unmeasured"], broken)
	if len(beyond) > 0 {
		fmt.Fprintf(&b, "Runs on Skiff although plain Compose fails, not counted above: %s.\n\n", strings.Join(beyond, ", "))
	}
	b.WriteString("| Project | Kind | Result | Detail |\n|---|---|---|---|\n")
	for _, r := range results {
		outcome, detail := r.Outcome, r.Detail
		if r.Control != "" && r.Outcome == "runs" {
			outcome, detail = "runs, though not under Compose", r.Control
		} else if r.Control != "" {
			outcome, detail = "not running under Compose", r.Control
		}
		detail = strings.NewReplacer("|", "/", "\n", " ").Replace(detail)
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", r.Name, r.Kind, outcome, detail[:min(len(detail), 300)])
	}
	if out := os.Getenv("SKIFF_COMPAT_OUT"); out != "" {
		if err := os.WriteFile(path.Join(out, "summary.md"), []byte(b.String()), 0o644); err != nil {
			t.Error(err)
		}
	}
}

func compatGit(dir string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-c", "filter.lfs.clean=", "-c", "filter.lfs.smudge=", "-c", "filter.lfs.process=", "-c", "filter.lfs.required=false"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_LFS_SKIP_SMUDGE=1", "GIT_LFS_SKIP_PUSH=1", "GIT_AUTHOR_NAME=e2e", "GIT_AUTHOR_EMAIL=e2e@example.com", "GIT_COMMITTER_NAME=e2e", "GIT_COMMITTER_EMAIL=e2e@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %v: %s", args[0], err, out)
	}
	return nil
}

func compatCheckout(p compatProject, work string) error {
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	for _, step := range [][]string{
		{"init", "-q", "--template=", "-b", "main"},
		{"fetch", "-q", "--depth", "1", "https://github.com/" + p.Repo + ".git", p.Commit},
		{"checkout", "-q", "-B", "main", "FETCH_HEAD"},
	} {
		if err := compatGit(work, step...); err != nil {
			return err
		}
	}
	// A shallow history can't be pushed, so the pinned files become a single new commit.
	return os.RemoveAll(filepath.Join(work, ".git"))
}

// compatPublish serves the checkout, with the .env someone following the project's instructions would create.
func compatPublish(gitRoot, work string, r compatPlanResult) error {
	dir := filepath.Join(work, filepath.Dir(r.File))
	if _, err := os.Stat(filepath.Join(dir, ".env")); err != nil {
		var b strings.Builder
		for k, v := range r.env {
			fmt.Fprintf(&b, "%s=%s\n", k, v)
		}
		if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(b.String()), 0o644); err != nil {
			return err
		}
	}
	for _, file := range r.envFiles {
		path := filepath.Join(dir, file)
		if !strings.HasPrefix(path, work+string(filepath.Separator)) {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if example := compatEnvExample(path); example != "" {
			data, err := os.ReadFile(example)
			if err != nil {
				return err
			}
			if err := os.WriteFile(path, data, 0o644); err != nil {
				return err
			}
		}
	}
	for _, step := range [][]string{
		{"init", "-q", "--template=", "-b", "main"},
		{"add", "-A", "-f"},
		{"commit", "-q", "-m", r.Commit},
	} {
		if err := compatGit(work, step...); err != nil {
			return err
		}
	}
	bare := filepath.Join(gitRoot, r.Name+".git")
	if err := compatGit(gitRoot, "init", "-q", "--template=", "--bare", "-b", "main", bare); err != nil {
		return err
	}
	return compatGit(work, "push", "-q", "--force", bare, "main")
}

// compatAnswers checks the services people reach, from inside the stack's network. Any answer short of a server error counts, since most apps want a login first.
func compatAnswers(r compatPlanResult, appOf func(string) string) string {
	var targets []compose.Item
	for _, it := range r.plan.Items {
		if it.Kind == compose.KindApp && it.Port > 0 && it.Public {
			targets = append(targets, it)
		}
	}
	if len(targets) == 0 {
		for _, it := range r.plan.Items {
			if it.Kind == compose.KindApp && it.Port > 0 {
				targets = append(targets, it)
			}
		}
	}
	if len(targets) == 0 {
		return ""
	}
	var why string
	for _, it := range targets {
		why = compatAnswer(appOf(it.Service), it)
		if why != "" {
			return why
		}
	}
	return ""
}

var compatStatus = regexp.MustCompile(`HTTP/[\d.]+ (\d{3})`)

func compatAnswer(app string, it compose.Item) string {
	out, _ := exec.Command("docker", "ps", "-q", "--filter", "label=skiff.app="+app).Output()
	ids := strings.Fields(string(out))
	if len(ids) == 0 {
		return it.Service + " has no running container"
	}
	nets, _ := exec.Command("docker", "inspect", "-f", "{{range $k, $v := .NetworkSettings.Networks}}{{$k}} {{end}}", ids[0]).Output()
	f := strings.Fields(string(nets))
	if len(f) == 0 {
		return it.Service + " has no network"
	}
	// A private service with no HTTP health check may not speak HTTP at all (a game server, gRPC), so, like Skiff, it counts once its port opens.
	tcp := !it.Public && it.HealthPath == ""
	var last string
	for deadline := time.Now().Add(3 * time.Minute); time.Now().Before(deadline); time.Sleep(5 * time.Second) {
		if tcp {
			got, err := exec.Command("docker", "run", "--rm", "--network", f[0], "busybox:1.36", "nc", "-z", "-w", "10", it.Service, strconv.Itoa(it.Port)).CombinedOutput()
			if err == nil {
				return ""
			}
			last = "port closed " + strings.Join(strings.Fields(string(got)), " ")
			continue
		}
		got, _ := exec.Command("docker", "run", "--rm", "--network", f[0], "busybox:1.36", "wget", "-S", "-T", "10", "-O", "/dev/null", fmt.Sprintf("http://%s:%d/", it.Service, it.Port)).CombinedOutput()
		last = strings.Join(strings.Fields(string(got)), " ")
		if m := compatStatus.FindStringSubmatch(last); m != nil && m[1] < "500" {
			return ""
		}
	}
	return fmt.Sprintf("%s:%d didn't answer: %s", it.Service, it.Port, last[:min(len(last), 200)])
}

func compatWrite(t *testing.T, name string, results any) {
	t.Helper()
	out := os.Getenv("SKIFF_COMPAT_OUT")
	if out == "" {
		return
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.MarshalIndent(results, "", " ")
	if err := os.WriteFile(path.Join(out, name+".json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}
