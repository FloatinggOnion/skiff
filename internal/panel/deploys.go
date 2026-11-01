package panel

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nuelScript/skiff/internal/config"
	"github.com/nuelScript/skiff/internal/github"
)

// inflightBuild is the app's currently-building deploy; a newer deploy supersedes (cancels) an older one still building instead of racing it.
type inflightBuild struct {
	id     string
	cancel context.CancelFunc
}

var (
	inflightMu sync.Mutex
	inflight   = map[string]inflightBuild{}
)

// deployDeadline bounds the whole pipeline (clone+build+release); set above the build's own 15-minute cap so it only fires on a genuine hang, freeing the in-flight slot rather than leaving a deploy stuck "building".
const deployDeadline = 30 * time.Minute

func cancelInflight(app, id string) bool {
	inflightMu.Lock()
	defer inflightMu.Unlock()
	if b, ok := inflight[app]; ok && (id == "" || b.id == id) {
		b.cancel()
		return true
	}
	return false
}

func building(app string) bool {
	inflightMu.Lock()
	defer inflightMu.Unlock()
	_, ok := inflight[app]
	return ok
}

// beginBuild registers this build as the app's in-flight one, canceling any prior deploy OR rollback so their container swaps never race. Returns a build context, a "was I superseded" predicate, and a cleanup to defer.
func beginBuild(app, id string) (context.Context, func() bool, func()) {
	ctx, cancel := context.WithTimeout(context.Background(), deployDeadline)
	inflightMu.Lock()
	if prev, ok := inflight[app]; ok {
		prev.cancel()
	}
	inflight[app] = inflightBuild{id: id, cancel: cancel}
	inflightMu.Unlock()
	cleanup := func() {
		inflightMu.Lock()
		if b, ok := inflight[app]; ok && b.id == id {
			delete(inflight, app)
		}
		inflightMu.Unlock()
		cancel()
	}
	return ctx, func() bool { return ctx.Err() != nil }, cleanup
}

// runDeploy clones, builds, and releases a source (manual deploys and webhooks both use it). authURL overrides the clone URL when set (e.g. a pasted token); otherwise repo sources use a GitHub-App token.
func (p *Panel) runDeploy(src Source, authURL, commit, message, trigger, id string) {
	logp := logPath(src.App, id)
	if err := os.MkdirAll(filepath.Dir(logp), 0o755); err != nil {
		return
	}
	f, err := os.Create(logp)
	if err != nil {
		return
	}
	defer f.Close()
	logln := func(s string) { fmt.Fprintln(f, s) }

	addDeploy(Deploy{
		ID: id, App: src.App, Commit: shortCommit(commit), Message: message,
		Trigger: trigger, Status: "building", Started: time.Now().Unix(),
	})

	ctx, superseded, done := beginBuild(src.App, id)
	defer done()
	go p.commentOnPR(src, shortCommit(commit), "building")
	sha := commit
	status, stopStatus := p.statusReporter(src)
	defer stopStatus()
	status(sha, "building")
	// finish maps a supersede (canceled by a newer deploy) to "canceled", not "failed" — no failure alert.
	finish := func(failMsg string) {
		if superseded() {
			logln("✗ superseded by a newer deploy")
			setDeployStatus(src.App, id, "canceled")
			status(sha, "canceled")
			return
		}
		logln(failMsg)
		setDeployStatus(src.App, id, "failed")
		go p.commentOnPR(src, shortCommit(commit), "failed")
		status(sha, "failed")
		go dispatchAlert(alertEvent{
			Team: src.Team, Kind: "deploy.failed", App: src.App,
			Title:  "Deploy failed: " + src.App,
			Detail: strings.TrimPrefix(strings.TrimSpace(failMsg), "✗ "),
		})
	}

	clone := authURL
	if clone == "" {
		clone = src.CloneURL
		if src.Repo != "" {
			if gh := github.Load(); gh.Installed() {
				if u, e := gh.CloneURLWithToken(src.CloneURL); e == nil {
					clone = u
				}
			}
		}
	}

	work := filepath.Join(p.buildsDir, sanitizeName(src.App))
	_ = os.RemoveAll(work)

	label := src.Repo
	if label == "" {
		label = src.CloneURL
	}
	logln("→ cloning " + label + "  (" + orMain(src.Branch) + ")")
	args := []string{"clone", "--depth", "1"}
	if src.Branch != "" {
		args = append(args, "--branch", src.Branch)
	}
	args = append(args, clone, work)
	cl := exec.CommandContext(ctx, "git", args...)
	// GIT_ALLOW_PROTOCOL restricts clone to http(s) so a crafted URL can't reach git's command-executing transports (ext::, file::) even if it slips past the scheme check.
	cl.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ALLOW_PROTOCOL=https:http")
	if out, e := cl.CombinedOutput(); e != nil {
		finish("✗ clone failed: " + cloneError(out))
		return
	}
	if sha == "" {
		if out, err := exec.Command("git", "-C", work, "rev-parse", "HEAD").Output(); err == nil {
			sha = strings.TrimSpace(string(out))
			status(sha, "building")
		}
	}

	// Root dir (monorepo support), joined safely so a "../.." can't escape the clone.
	ctxDir := filepath.Join(work, filepath.Clean("/"+src.RootDir))
	if fi, err := os.Stat(ctxDir); err != nil || !fi.IsDir() {
		finish("✗ root directory not found in the repo: " + orRoot(src.RootDir))
		return
	}
	tomlPath := filepath.Join(ctxDir, "skiff.toml")
	_ = os.WriteFile(tomlPath, []byte(projectToml(src, deployEnv(src), teamNetwork(src.Team), p.volumeDefs(src))), 0o644)

	logln("→ building & deploying")
	self, _ := os.Executable()
	cmd := exec.CommandContext(ctx, self, "deploy", "-c", tomlPath)
	// On supersede, SIGINT so the build cancels gracefully; WaitDelay hard-kills it if it doesn't exit promptly.
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 10 * time.Second
	cmd.Env = append(os.Environ(), "SKIFF_DEPLOY_ID="+id) // retain this build for rollback
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	errc := make(chan error, 1)
	go func() { errc <- cmd.Run(); _ = pw.Close() }()
	sc := bufio.NewScanner(pr)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		logln(sc.Text())
	}
	// Drain the remainder (e.g. a line past the scanner's 1 MB cap stops Scan early) so the child can't block on a full pipe and wedge the build.
	_, _ = io.Copy(io.Discard, pr)
	if e := <-errc; e != nil {
		finish("✗ deploy failed")
		return
	}
	logln("✓ live")
	setDeployStatus(src.App, id, "live")
	p.reconcileWorkers(src.App)
	p.pruneVolumes(src)
	go p.commentOnPR(src, shortCommit(commit), "live")
	status(sha, "live")
}

func (p *Panel) runRollback(src Source, targetID, commit, message, id string) {
	logp := logPath(src.App, id)
	if err := os.MkdirAll(filepath.Dir(logp), 0o755); err != nil {
		return
	}
	f, err := os.Create(logp)
	if err != nil {
		return
	}
	defer f.Close()
	logln := func(s string) { fmt.Fprintln(f, s) }

	addDeploy(Deploy{
		ID: id, App: src.App, Commit: commit, Message: message,
		Trigger: "rollback", Status: "building", Started: time.Now().Unix(),
	})

	ctx, superseded, done := beginBuild(src.App, id)
	defer done()

	work := filepath.Join(p.buildsDir, sanitizeName(src.App)+"-rollback")
	_ = os.RemoveAll(work)
	if err := os.MkdirAll(work, 0o755); err != nil {
		logln("✗ rollback failed: " + err.Error())
		setDeployStatus(src.App, id, "failed")
		return
	}
	tomlPath := filepath.Join(work, "skiff.toml")
	env, net := p.appRuntime(src)
	_ = os.WriteFile(tomlPath, []byte(projectToml(src, envList(env), net, p.volumeDefs(src))), 0o644)

	image := fmt.Sprintf("skiff-%s:%s", src.App, targetID)
	logln("→ rolling back to " + image)
	self, _ := os.Executable()
	cmd := exec.CommandContext(ctx, self, "rollback", "--image", image, "-c", tomlPath)
	cmd.Env = append(os.Environ(), "SKIFF_DEPLOY_ID="+id)
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 10 * time.Second
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	errc := make(chan error, 1)
	go func() { errc <- cmd.Run(); _ = pw.Close() }()
	sc := bufio.NewScanner(pr)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		logln(sc.Text())
	}
	// Drain the remainder (e.g. a line past the scanner's 1 MB cap stops Scan early) so the child can't block on a full pipe and wedge the build.
	_, _ = io.Copy(io.Discard, pr)
	if e := <-errc; e != nil {
		if superseded() {
			logln("✗ superseded by a newer deploy")
			setDeployStatus(src.App, id, "canceled")
			return
		}
		logln("✗ rollback failed")
		setDeployStatus(src.App, id, "failed")
		return
	}
	logln("✓ live")
	setDeployStatus(src.App, id, "live")
	p.reconcileWorkers(src.App)
	p.pruneVolumes(src)
}

func (p *Panel) tailLog(w http.ResponseWriter, r *http.Request, app, id string) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")

	logp := logPath(app, id)
	sent := 0
	flush := func() {
		for _, ln := range readLogLines(logp)[sent:] {
			fmt.Fprintf(w, "data: %s\n\n", ln)
			sent++
		}
		fl.Flush()
	}
	for {
		flush()
		switch deployStatus(app, id) {
		case "live":
			flush()
			fmt.Fprint(w, "data: [done] ok\n\n")
			fl.Flush()
			return
		case "failed":
			flush()
			fmt.Fprint(w, "data: [done] error\n\n")
			fl.Flush()
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(400 * time.Millisecond):
		}
	}
}

// controlPlaneApp is the pseudo-app Skiff records its own self-deploys under — no source/team, so any signed-in user may inspect it.
const controlPlaneApp = "panel"

func (p *Panel) canViewDeploys(r *http.Request, app string) bool {
	return app == controlPlaneApp || p.canAccess(r, app)
}

func (p *Panel) handleDeployLog(w http.ResponseWriter, r *http.Request) {
	app := sanitizeName(r.URL.Query().Get("app"))
	if !p.canViewDeploys(r, app) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	id := sanitizeID(r.URL.Query().Get("id"))
	p.tailLog(w, r, app, id)
}

func (p *Panel) handleDeploys(w http.ResponseWriter, r *http.Request) {
	app := sanitizeName(r.URL.Query().Get("app"))
	before, limit := pageBounds(r)
	beforeID := r.URL.Query().Get("beforeId")
	w.Header().Set("Content-Type", "application/json")
	if app == "" {
		_ = json.NewEncoder(w).Encode(teamDeploys(p.teamID(r), before, beforeID, limit))
		return
	}
	if !p.canViewDeploys(r, app) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	deploys := appDeploysPage(app, before, beforeID, limit)
	p.markRollbackable(app, deploys)
	_ = json.NewEncoder(w).Encode(deploys)
}

// pageBounds reads a ?before cursor and a clamped ?limit (default 30, max 100).
func pageBounds(r *http.Request) (before int64, limit int) {
	before, _ = strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	limit = 30
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 {
		if n > 100 {
			n = 100
		}
		limit = n
	}
	return
}

func readLogLines(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	s := strings.TrimRight(string(b), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func newDeployID() string {
	return fmt.Sprintf("%d-%s", time.Now().Unix(), randHex(3))
}

func shortCommit(c string) string {
	if len(c) > 7 {
		return c[:7]
	}
	return c
}

func orMain(b string) string {
	if b == "" {
		return "default branch"
	}
	return b
}

func orRoot(d string) string {
	if d == "" {
		return "/"
	}
	return d
}

// projectToml renders the deploy skiff.toml; env vars split into [env] (build+runtime) vs [secrets] (runtime-only, never baked into the image).
func projectToml(src Source, env []EnvVar, network string, volumes []config.Volume) string {
	var b strings.Builder
	fmt.Fprintf(&b, "name = %q\n", src.App)
	if src.Replicas > 1 {
		fmt.Fprintf(&b, "replicas = %d\n", src.Replicas)
	}
	fmt.Fprintf(&b, "\n[build]\nport = %s\n", src.Port)
	b.WriteString("\n[deploy]\n")
	fmt.Fprintf(&b, "network = %q\n", network)
	if strings.TrimSpace(src.Release) != "" {
		fmt.Fprintf(&b, "release = %q\n", src.Release)
	}
	if hc := healthOf(src); hc.HealthPath != "" || hc.HealthTimeout > 0 {
		if hc.HealthPath != "" {
			fmt.Fprintf(&b, "health_path = %q\n", hc.HealthPath)
		}
		if hc.HealthTimeout > 0 {
			fmt.Fprintf(&b, "health_timeout = %d\n", hc.HealthTimeout)
		}
	}
	var buildVars, secretVars []EnvVar
	for _, e := range env {
		if e.Build {
			buildVars = append(buildVars, e)
		} else {
			secretVars = append(secretVars, e)
		}
	}
	if len(buildVars) > 0 {
		b.WriteString("\n[env]\n")
		for _, e := range buildVars {
			fmt.Fprintf(&b, "%s = %q\n", e.Key, e.Value)
		}
	}
	if len(secretVars) > 0 {
		b.WriteString("\n[secrets]\n")
		for _, e := range secretVars {
			fmt.Fprintf(&b, "%s = %q\n", e.Key, e.Value)
		}
	}
	for _, v := range volumes {
		fmt.Fprintf(&b, "\n[[volumes]]\nname = %q\npath = %q\n", v.Name, v.Path)
	}
	return b.String()
}

func envList(m map[string]string) []EnvVar {
	out := make([]EnvVar, 0, len(m))
	for k, v := range m {
		out = append(out, EnvVar{Key: k, Value: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
