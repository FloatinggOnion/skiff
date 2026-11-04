package panel

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nuelScript/skiff/internal/github"
)

func baseURL(r *http.Request) string {
	scheme := "https"
	if strings.HasPrefix(r.Host, "localhost") || strings.HasPrefix(r.Host, "127.0.0.1") {
		scheme = "http"
	}
	return scheme + "://" + r.Host
}

func (p *Panel) handleGithubStatus(w http.ResponseWriter, r *http.Request) {
	cfg := github.Load()
	out := map[string]any{
		"configured": cfg.Configured(),
		"installed":  cfg.Installed() && githubUsable(p.teamID(r)),
		"usable":     githubUsable(p.teamID(r)),
	}
	if cfg.Configured() {
		out["slug"] = cfg.Slug
		if prGate.needsGrant() || statusGate.needsGrant() {
			out["permissionsUrl"] = "https://github.com/settings/apps/" + cfg.Slug + "/permissions"
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (p *Panel) handleGithubCreate(w http.ResponseWriter, r *http.Request) {
	if !p.isInstanceAdmin(r) {
		http.Error(w, "only this instance's owner can connect GitHub", http.StatusForbidden)
		return
	}
	name := "Skiff Deploys (" + p.domain + ")"
	manifest := github.Manifest(baseURL(r), name)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><body style="background:#000;color:#aaa;font-family:system-ui">
<form id="f" action="https://github.com/settings/apps/new" method="post">
<input type="hidden" name="manifest" value='%s'>
</form>
<p style="padding:40px">Redirecting to GitHub…</p>
<script>document.getElementById('f').submit()</script></body>`, manifest)
}

func (p *Panel) handleGithubCreated(w http.ResponseWriter, r *http.Request) {
	if !p.isInstanceAdmin(r) {
		http.Error(w, "only this instance's owner can connect GitHub", http.StatusForbidden)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}
	cfg, err := github.ConvertManifest(code)
	if err != nil {
		http.Error(w, "could not complete GitHub setup", http.StatusBadGateway)
		return
	}
	if err := cfg.Save(); err != nil {
		http.Error(w, "could not save GitHub configuration", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, cfg.InstallURL(), http.StatusFound)
}

func (p *Panel) handleGithubInstalled(w http.ResponseWriter, r *http.Request) {
	if !p.isInstanceAdmin(r) {
		http.Error(w, "only this instance's owner can connect GitHub", http.StatusForbidden)
		return
	}
	cfg := github.Load()
	if !cfg.Configured() {
		http.Error(w, "app not configured", http.StatusBadRequest)
		return
	}
	id, _ := strconv.ParseInt(r.URL.Query().Get("installation_id"), 10, 64)
	cfg.InstallationID = id
	if err := cfg.Save(); err != nil {
		http.Error(w, "could not save GitHub configuration", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func (p *Panel) handleGithubRepos(w http.ResponseWriter, r *http.Request) {
	cfg := github.Load()
	if !cfg.Installed() || !githubUsable(p.teamID(r)) {
		http.Error(w, "github not connected", http.StatusBadRequest)
		return
	}
	repos, err := cfg.ListRepos()
	if err != nil {
		http.Error(w, "could not list repositories", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(repos)
}

func (p *Panel) handleGithubDeploy(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	repo := strings.TrimSpace(q.Get("repo"))
	clone := strings.TrimSpace(q.Get("clone"))
	name := sanitizeName(q.Get("name"))
	branch := strings.TrimSpace(q.Get("branch"))
	rootDir := strings.TrimSpace(q.Get("rootdir"))
	port := strings.TrimSpace(q.Get("port"))
	if repo == "" || name == "" {
		http.Error(w, "repo and name are required", http.StatusBadRequest)
		return
	}
	if !github.ValidRepo(repo) {
		http.Error(w, "repo must look like owner/name", http.StatusBadRequest)
		return
	}
	if clone == "" {
		clone = github.CloneURL(repo)
	}
	if !github.IsCloneURLOf(clone, repo) {
		http.Error(w, "clone url doesn't match the repository", http.StatusBadRequest)
		return
	}
	if !validPort(port) {
		http.Error(w, "port must be a number from 1 to 65535", http.StatusBadRequest)
		return
	}
	team := p.teamID(r)
	if !githubUsable(team) {
		http.Error(w, "the GitHub connection belongs to the team that set up this server: deploy from a git URL with your own token instead", http.StatusForbidden)
		return
	}
	existing, ok := getSource(name)
	if ok && existing.Team != team {
		http.Error(w, "an app with that name exists in another team", http.StatusConflict)
		return
	}
	if !ok && nameReserved(name, team) {
		http.Error(w, reservedNameMsg, http.StatusBadRequest)
		return
	}
	if !ok && envStage.heldByOther(name, team, time.Now()) {
		// Discard env another team staged under this unused name so this deploy can't inherit foreign vars.
		_ = setEnv(name, nil)
	}
	if port == "" {
		port = "3000"
	}
	src := Source{
		App: name, Team: team, Repo: repo, Branch: branch, RootDir: rootDir,
		Port: port, CloneURL: clone, Auto: q.Get("auto") == "1",
	}
	if err := putSource(src); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	envStage.release(name)
	id := newDeployID()
	src.actor = p.deployActor(r)
	go p.runDeploy(src, "", "", "", "manual", id)
	p.tailLog(w, r, name, id)
}

func (p *Panel) onPush(push github.Push) {
	if push.Deleted {
		for _, pv := range branchPreviews(push.Repo, push.Branch) {
			recordAudit(pv.Team, AuditEntry{Actor: "push", Action: "project.delete", Target: pv.App, Detail: "branch deleted"})
			go p.removeApp(pv.App)
		}
		p.removeBranchStacks(push.Repo, push.Branch, "branch deleted")
		return
	}
	for _, name := range stacksForPush(push.Repo, push.Branch) {
		stack := name
		go guard("syncStackOnPush", func() { p.syncStackOnPush(stack, push) })
	}
	for _, src := range sourcesForRepo(push.Repo, push.Branch) {
		if src.Stack != "" || !touched(push.Paths, src.RootDir) {
			continue
		}
		id := newDeployID()
		recordAudit(src.Team, AuditEntry{Actor: "push", Action: "deploy", Target: src.App, Detail: shortCommit(push.Commit)})
		src.actor = push.Pusher
		go p.runDeploy(src, "", push.Commit, push.Message, "push", id)
	}
	for _, app := range previewsToCreate(push) {
		p.createPreview(app, push.Branch, push.Commit, push.Message, push.Pusher)
	}
	toSelfBranch := p.selfRepo != "" && push.Repo == p.selfRepo && push.Branch == p.selfBranch
	if toSelfBranch && p.pushTouchesSelf(push.Paths) {
		id := newDeployID()
		addDeploy(Deploy{
			ID: id, App: "panel", Commit: shortCommit(push.Commit), Message: push.Message,
			Trigger: "push", Status: "building", Started: time.Now().Unix(),
		})
		p.launchSelfUpdate(id, push.Commit)
	}
}

// onPullRequest posts the preview comment on a pull request opened after its branch's preview was built.
func (p *Panel) onPullRequest(pr github.PullRequest) {
	p.onStackPullRequest(pr)
	switch pr.Action {
	case "opened", "reopened", "ready_for_review":
	case "closed":
		for _, pv := range branchPreviews(pr.Repo, pr.Branch) {
			recordAudit(pv.Team, AuditEntry{Actor: "push", Action: "project.delete", Target: pv.App, Detail: "pull request closed"})
			go p.removeApp(pv.App)
		}
		return
	default:
		return
	}
	for _, pv := range branchPreviews(pr.Repo, pr.Branch) {
		if ds := appDeploys(pv.App); len(ds) > 0 {
			go p.commentOnPR(pv, ds[0].Commit, ds[0].Status)
		}
	}
}

// handleHook is the GitHub webhook receiver — HMAC-verified, not session-gated.
func (p *Panel) handleHook(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 5<<20))
	cfg := github.Load()
	if !cfg.Configured() {
		w.WriteHeader(http.StatusOK)
		return
	}
	// An empty secret makes the HMAC forgeable (empty key), so reject rather than accept unsigned pushes.
	if cfg.WebhookSecret == "" {
		http.Error(w, "webhook secret not configured", http.StatusServiceUnavailable)
		return
	}
	if !github.VerifySignature(cfg.WebhookSecret, body, r.Header.Get("X-Hub-Signature-256")) {
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return
	}
	if !hookDeliveries.firstSeen(r.Header.Get("X-GitHub-Delivery")) {
		w.WriteHeader(http.StatusOK)
		return
	}
	switch r.Header.Get("X-GitHub-Event") {
	case "push":
		if push, ok := github.ParsePush(body); ok {
			p.onPush(push)
		}
	case "pull_request":
		if pr, ok := github.ParsePullRequest(body); ok {
			p.onPullRequest(pr)
		}
	}
	w.WriteHeader(http.StatusOK)
}
