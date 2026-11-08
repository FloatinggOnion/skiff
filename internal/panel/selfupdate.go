package panel

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/nuelScript/skiff/internal/db"
	"github.com/nuelScript/skiff/internal/github"
)

const (
	portBlue  = "7070"
	portGreen = "7071"
)

type SelfUpdateOpts struct {
	Repo     string
	Branch   string
	Commit   string
	DeployID string
}

func SelfUpdate(opts SelfUpdateOpts) error {
	if sqlDB == nil {
		d, err := db.Open()
		if err != nil {
			return err
		}
		sqlDB = d
	}
	if opts.Branch == "" {
		opts.Branch = "main"
	}
	id := opts.DeployID
	if id == "" {
		id = newDeployID()
	}

	logp := logPath("panel", id)
	_ = os.MkdirAll(filepath.Dir(logp), 0o755)
	f, _ := os.OpenFile(logp, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	log := func(format string, a ...any) {
		if f != nil {
			fmt.Fprintln(f, fmt.Sprintf(format, a...))
		}
	}
	if deployStatus("panel", id) == "" {
		addDeploy(Deploy{
			ID: id, App: "panel", Commit: shortCommit(opts.Commit),
			Trigger: "push", Status: "building", Started: time.Now().Unix(),
		})
	}
	fail := func(format string, a ...any) error {
		log("✗ %s", fmt.Sprintf(format, a...))
		setDeployStatus("panel", id, "failed")
		if f != nil {
			_ = f.Close()
		}
		return fmt.Errorf(format, a...)
	}

	// Single-flight: never let two self-deploys race on the binary.
	lock := filepath.Join(skiffDir(), "self-update.lock")
	lf, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return fail("a self-deploy is already in progress")
	}
	defer func() { _ = lf.Close(); _ = os.Remove(lock) }()

	src := filepath.Join(skiffDir(), "self-src")
	_ = os.RemoveAll(src)
	cloneURL := github.CloneURL(opts.Repo)
	var authEnv []string
	if gh := github.Load(); gh.Installed() {
		authEnv, _ = gh.CloneEnv(cloneURL, opts.Repo)
	}
	log("→ cloning %s (%s)", opts.Repo, opts.Branch)
	cl := exec.Command("git", "clone", "--depth", "1", "--branch", opts.Branch, cloneURL, src)
	cl.Env = append(append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ALLOW_PROTOCOL=https:http"), authEnv...)
	if out, e := cl.CombinedOutput(); e != nil {
		return fail("clone failed: %s", cloneError(out))
	}

	log("→ building dashboard")
	if e := runLogged(f, "docker", "run", "--rm",
		"-v", src+":/w", "-v", "skiff-npm:/root/.npm", "-w", "/w/web/dash",
		"node:22", "sh", "-c",
		"npm ci --no-audit --no-fund && "+
			"npm run build && rm -rf ../../internal/panel/dist && cp -r dist ../../internal/panel/dist",
	); e != nil {
		return fail("dashboard build failed (see log)")
	}

	log("→ compiling binary")
	if e := runLogged(f, "docker", "run", "--rm",
		"-e", "CGO_ENABLED=0", "-e", "GOTOOLCHAIN=local", "-e", "GOFLAGS=-buildvcs=false",
		"-v", src+":/w", "-v", "skiff-gobuild:/root/.cache/go-build", "-v", "skiff-gomod:/go/pkg/mod",
		"-w", "/w", "golang:1.26", "go", "build", "-o", "/w/skiff.new", ".",
	); e != nil {
		return fail("compile failed (see log)")
	}
	newBin := filepath.Join(src, "skiff.new")
	if out, e := run(newBin, "version"); e != nil {
		return fail("new binary failed its smoke test: %s", tailLine(out))
	}

	bin := selfBinPath()
	active := activePort()
	next := otherPort(active)
	log("→ installing new binary (live :%s → :%s)", active, next)
	waitForDeploys(selfUpdateWait, 5*time.Second, log)
	_, _ = run("cp", bin, bin+".prev") // rollback point
	if e := installBinary(newBin, bin); e != nil {
		return fail("could not install binary: %v", e)
	}

	log("→ starting new panel on :%s", next)
	if out, e := run("systemctl", "start", "skiff-panel@"+next); e != nil {
		restoreBinary(bin)
		return fail("could not start new panel: %s", tailLine(out))
	}

	log("→ health-checking :%s", next)
	if !healthy(next, 60*time.Second) {
		_, _ = run("systemctl", "stop", "skiff-panel@"+next)
		restoreBinary(bin)
		return fail("new panel never became healthy — rolled back, still serving :%s", active)
	}

	log("→ flipping router :%s → :%s", active, next)
	if e := os.WriteFile(pointerPath(), []byte("127.0.0.1:"+next+"\n"), 0o644); e != nil {
		_, _ = run("systemctl", "stop", "skiff-panel@"+next)
		restoreBinary(bin)
		return fail("could not repoint router: %v", e)
	}
	time.Sleep(4 * time.Second) // router cache TTL + in-flight drain
	log("→ draining old panel :%s", active)
	_, _ = run("systemctl", "stop", "skiff-panel@"+active)
	_, _ = run("systemctl", "enable", "skiff-panel@"+next)
	_, _ = run("systemctl", "disable", "skiff-panel@"+active)

	log("✓ live on :%s — zero downtime, sessions preserved", next)
	setDeployStatus("panel", id, "live")
	if f != nil {
		_ = f.Close()
	}
	return nil
}

// launchSelfUpdate runs the self-deploy as a detached child; KillMode=process on the panel units lets it survive the old panel's stop to finish the swap.
func (p *Panel) launchSelfUpdate(id, commit string) {
	self, err := os.Executable()
	if err != nil || self == "" {
		self = selfBinPath()
	}
	cmd := exec.Command(self, "self-update",
		"--repo", p.selfRepo, "--branch", p.selfBranch, "--commit", commit, "--deploy-id", id)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		setDeployStatus("panel", id, "failed")
		return
	}
	go func() { _ = cmd.Wait() }()
}

// pushTouchesSelf reports whether a push changed anything outside the marketing site (web/site/); an unknown/empty file list conservatively counts as a change.
// siteDirs are the websites deployed as their own projects, so a push that only touches them leaves the panel alone.
var siteDirs = []string{"web/site/", "web/docs/"}

func (p *Panel) pushTouchesSelf(paths []string) bool {
	if len(paths) == 0 {
		return true
	}
	for _, f := range paths {
		if !slices.ContainsFunc(siteDirs, func(d string) bool { return strings.HasPrefix(f, d) }) {
			return true
		}
	}
	return false
}

// touched reports whether a push changed anything under rootDir; an unknown/empty file list conservatively counts as a change.
func touched(paths []string, rootDir string) bool {
	if len(paths) == 0 {
		return true
	}
	prefix := strings.Trim(rootDir, "/")
	if prefix == "" {
		return true
	}
	for _, f := range paths {
		if f == prefix || strings.HasPrefix(f, prefix+"/") {
			return true
		}
	}
	return false
}

func selfBinPath() string {
	if v := os.Getenv("SKIFF_BIN"); v != "" {
		return v
	}
	return "/usr/local/bin/skiff"
}

func pointerPath() string { return filepath.Join(skiffDir(), "panel.addr") }

func activePort() string {
	if b, err := os.ReadFile(pointerPath()); err == nil {
		v := strings.TrimSpace(string(b))
		if i := strings.LastIndexByte(v, ':'); i >= 0 {
			v = v[i+1:]
		}
		if v == portGreen {
			return portGreen
		}
	}
	return portBlue
}

func otherPort(p string) string {
	if p == portBlue {
		return portGreen
	}
	return portBlue
}

// installBinary atomically replaces the live binary — safe while the old one runs, since running processes keep their in-memory copy.
func installBinary(newBin, dst string) error {
	tmp := dst + ".next"
	if out, err := run("cp", newBin, tmp); err != nil {
		return fmt.Errorf("%s", tailLine(out))
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

func restoreBinary(dst string) {
	if _, err := os.Stat(dst + ".prev"); err == nil {
		_, _ = run("cp", dst+".prev", dst)
	}
}

func healthy(port string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 3 * time.Second}
	for time.Now().Before(deadline) {
		if resp, err := client.Get("http://127.0.0.1:" + port + "/api/me"); err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
		time.Sleep(2 * time.Second)
	}
	return false
}

func run(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ALLOW_PROTOCOL=https:http")
	return cmd.CombinedOutput()
}

func runLogged(f *os.File, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ALLOW_PROTOCOL=https:http")
	if f != nil {
		cmd.Stdout, cmd.Stderr = f, f
	}
	return cmd.Run()
}

func tailLine(b []byte) string {
	last := ""
	for _, ln := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if s := strings.TrimSpace(ln); s != "" {
			last = s
		}
	}
	return last
}

// selfUpdateWait bounds how long a finished panel build holds back for app deploys; a merge often starts both, and restarting would cut the app's build short.
const selfUpdateWait = 15 * time.Minute

func waitForDeploys(limit, every time.Duration, log func(string, ...any)) {
	deadline := time.Now().Add(limit)
	logged := false
	for {
		running := buildingDeploys()
		if len(running) == 0 {
			return
		}
		if time.Now().After(deadline) {
			log("→ restarting with %d deploy(s) still running", len(running))
			return
		}
		if !logged {
			log("→ waiting for %d running deploy(s) to finish", len(running))
			logged = true
		}
		time.Sleep(every)
	}
}
