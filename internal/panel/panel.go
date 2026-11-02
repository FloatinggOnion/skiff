// Package panel is Skiff's login-gated control panel where teams deploy git projects onto the box, served behind the router at dash.<domain>.
package panel

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nuelScript/skiff/internal/auth"
	"github.com/nuelScript/skiff/internal/db"
	"github.com/nuelScript/skiff/internal/docker"
	"github.com/nuelScript/skiff/internal/reconcile"
)

type sess struct {
	userID string
	teamID string
}

type Panel struct {
	setupSecret string // gate on first-run setup, so randoms can't claim the box
	domain      string
	eng         *docker.Engine
	buildsDir   string
	auth        *auth.Store

	// selfRepo/selfBranch identify Skiff's own repo so a push to it hot-swaps the control plane; empty disables self-deploy.
	selfRepo   string
	selfBranch string
}

func New(setupSecret, domain string, eng *docker.Engine) (*Panel, error) {
	database, err := db.Open()
	if err != nil {
		return nil, err
	}
	sqlDB = database
	if err := initSeal(); err != nil {
		return nil, err
	}
	reconcileStuckDeploys()
	writeMailFile()
	home, _ := os.UserHomeDir()
	branch := os.Getenv("SKIFF_SELF_BRANCH")
	if branch == "" {
		branch = "main"
	}
	p := &Panel{
		setupSecret: setupSecret,
		domain:      domain,
		eng:         eng,
		buildsDir:   filepath.Join(home, ".skiff", "builds"),
		auth:        auth.NewStore(database),
		selfRepo:    os.Getenv("SKIFF_SELF_REPO"),
		selfBranch:  branch,
	}
	resStats = newResStore(filepath.Join(home, ".skiff", "resources.json"))
	go guard("reapOrphanContainers", p.reapOrphanContainers)
	go func() { _ = eng.EnsureNetwork(dbNetwork) }()
	go guard("reconcileNetworks", p.reconcileNetworks)
	go guard("recoverDatabases", p.recoverDatabases)
	go guard("prewarmDatabaseImages", p.prewarmDatabaseImages)
	go guard("prewarmStorageImages", p.prewarmStorageImages)
	go guard("writeGateFile", func() { _ = writeGateFile() })
	go guard("writeEdgeFile", func() { _ = writeEdgeFile() })
	go guard("removeStaleDeployConfigs", func() { removeStaleDeployConfigs(p.buildsDir, time.Now()) })
	go guard("dropCopiedPreviewEnv", dropCopiedPreviewEnv)
	go p.backupLoop()
	go p.jobLoop()
	go p.resourceLoop()
	go p.autoscaleLoop()
	go p.alertLoop()
	go p.gcLoop()
	go p.cliAppLoop()
	go p.requestsLoop()
	go p.uptimeLoop()
	go p.sleepLoop()
	go p.cpBackupLoop()
	go p.appLogLoop()
	go p.logDrainLoop()
	go p.oomLoop()
	return p, nil
}

// reapOrphanContainers clears leftover skiff containers and re-registers running
// apps the registry lost track of, skipping recent ones so an in-progress deploy isn't hit.
func (p *Panel) reapOrphanContainers() {
	res, err := reconcile.Reap(p.eng, time.Now().Add(-10*time.Minute))
	if err != nil {
		log.Printf("skiff: reaping orphan containers: %v", err)
	}
	for _, app := range res.Adopted {
		log.Printf("skiff: adopted running app %q the registry had lost track of", app)
	}
}

func (p *Panel) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/me", p.handleMe)
	mux.HandleFunc("/api/auth/setup", p.handleSetup)
	mux.HandleFunc("/api/auth/login", p.handleLogin)
	mux.HandleFunc("/api/auth/login/code", p.handleLoginCode)
	mux.HandleFunc("/api/auth/logout", p.handleLogout)
	mux.HandleFunc("/api/auth/accept", p.handleAccept)
	mux.HandleFunc("/api/cli/login", p.handleCLILogin)
	mux.HandleFunc("/api/cli/approve", p.protected(p.handleCLIApprove))
	mux.HandleFunc("/api/auth/team", p.protected(p.handleTeamSwitch))
	mux.HandleFunc("/api/account", p.protected(p.handleAccount))
	mux.HandleFunc("/api/account/password", p.protected(p.handlePassword))
	mux.HandleFunc("/api/account/2fa", p.protected(p.handleTwoFactor))
	mux.HandleFunc("/api/account/delete", p.protected(p.handleAccountDelete))
	mux.HandleFunc("/api/teams", p.protected(p.handleTeamCreate))
	mux.HandleFunc("/api/teams/rename", p.protected(p.handleTeamRename))
	mux.HandleFunc("/api/teams/leave", p.protected(p.handleTeamLeave))
	mux.HandleFunc("/api/teams/delete", p.protected(p.handleTeamDelete))
	mux.HandleFunc("/api/teams/members", p.protected(p.handleMembers))
	mux.HandleFunc("/api/teams/invite", p.protected(p.handleInvite))
	mux.HandleFunc("/api/system", p.protected(p.handleSystem))
	mux.HandleFunc("/api/server", p.protected(p.handleServer))
	mux.HandleFunc("/api/server/gc", p.protected(p.handleServerGC))
	mux.HandleFunc("/api/server/usage", p.protected(p.handleServerUsage))
	mux.HandleFunc("/api/overview", p.protected(p.handleOverview))
	mux.HandleFunc("/api/control-plane", p.protected(p.handleControlPlane))
	mux.HandleFunc("/api/apps", p.protected(p.handleApps))
	mux.HandleFunc("/api/project", p.protected(p.handleProject))
	mux.HandleFunc("/api/project/preview-password", p.protected(p.handlePreviewPassword))
	mux.HandleFunc("/api/project/registry", p.protected(p.handleRegistry))
	mux.HandleFunc("/api/projects/image", p.protected(p.handleImageProject))
	mux.HandleFunc("/api/catalog", p.protected(p.handleCatalog))
	mux.HandleFunc("/api/status-page", p.protected(p.handleStatusPage))
	mux.HandleFunc("/public/status/", p.handlePublicStatus)
	mux.HandleFunc("/api/redeploy", p.protected(p.handleRedeploy))
	mux.HandleFunc("/api/rollback", p.protected(p.handleRollback))
	mux.HandleFunc("/api/cancel", p.protected(p.handleCancel))
	mux.HandleFunc("/api/exec", p.protected(p.handleExec))
	mux.HandleFunc("/api/databases", p.protected(p.handleDatabases))
	mux.HandleFunc("/api/databases/attach", p.protected(p.handleDatabaseAttach))
	mux.HandleFunc("/api/databases/public", p.protected(p.handleDatabasePublic))
	mux.HandleFunc("/api/databases/unmanaged", p.protected(p.handleUnmanagedDatabases))
	mux.HandleFunc("/api/storage", p.protected(p.handleStorage))
	mux.HandleFunc("/api/storage/attach", p.protected(p.handleStorageAttach))
	mux.HandleFunc("/api/storage/objects", p.protected(p.handleBucketObjects))
	mux.HandleFunc("/api/storage/object", p.protected(p.handleBucketObject))
	mux.HandleFunc("/api/backups", p.protected(p.handleBackups))
	mux.HandleFunc("/api/backups/restore", p.protected(p.handleBackupRestore))
	mux.HandleFunc("/api/backups/download", p.protected(p.handleBackupDownload))
	mux.HandleFunc("/api/backups/offsite", p.protected(p.handleOffsite))
	mux.HandleFunc("/api/jobs", p.protected(p.handleJobs))
	mux.HandleFunc("/api/jobs/run", p.protected(p.handleJobRun))
	mux.HandleFunc("/api/workers", p.protected(p.handleWorkers))
	mux.HandleFunc("/api/volumes", p.protected(p.handleVolumes))
	mux.HandleFunc("/api/db/exec", p.protected(p.handleDBShell))
	mux.HandleFunc("/api/domains", p.protected(p.handleDomains))
	mux.HandleFunc("/api/mail", p.protected(p.handleMail))
	mux.HandleFunc("/api/preview", p.protected(p.handleCreatePreview))
	mux.HandleFunc("/api/shared-env", p.protected(p.handleSharedEnv))
	mux.HandleFunc("/api/analytics", p.protected(p.handleAnalytics))
	mux.HandleFunc("/api/resources", p.protected(p.handleResources))
	mux.HandleFunc("/api/insights", p.protected(p.handleInsights))
	mux.HandleFunc("/api/cost", p.protected(p.handleCost))
	mux.HandleFunc("/api/uptime", p.protected(p.handleUptime))
	mux.HandleFunc("/api/alerts", p.protected(p.handleAlerts))
	mux.HandleFunc("/api/alerts/test", p.protected(p.handleAlertTest))
	mux.HandleFunc("/api/log-drain", p.protected(p.handleLogDrain))
	mux.HandleFunc("/api/log-drain/test", p.protected(p.handleLogDrainTest))
	mux.HandleFunc("/api/audit", p.protected(p.handleAudit))
	mux.HandleFunc("/api/tokens", p.protected(p.handleTokens))

	mux.HandleFunc("GET /api/v1/apps", p.apiAuth(p.apiListApps))
	mux.HandleFunc("GET /api/v1/apps/{name}", p.apiAuth(p.apiGetApp))
	mux.HandleFunc("POST /api/v1/apps/{name}/deploy", p.apiAuth(p.apiDeploy))
	mux.HandleFunc("GET /api/v1/apps/{name}/env", p.apiAuth(p.apiEnv))
	mux.HandleFunc("PUT /api/v1/apps/{name}/env", p.apiAuth(p.apiEnv))
	mux.HandleFunc("GET /api/v1/deploys/{id}", p.apiAuth(p.apiDeployStatus))
	mux.HandleFunc("GET /api/v1/deploys/{id}/log", p.apiAuth(p.apiDeployLog))
	mux.HandleFunc("GET /api/v1/apps/{name}/deploys", p.apiAuth(p.apiDeploys))
	mux.HandleFunc("GET /api/v1/apps/{name}/logs", p.apiAuth(p.apiLogs))
	mux.HandleFunc("POST /api/v1/apps/{name}/rollback", p.apiAuth(p.apiRollback))
	mux.HandleFunc("GET /api/v1/apps/{name}/insights", p.apiAuth(p.apiInsights))
	mux.HandleFunc("POST /api/v1/cli/deploys", p.apiAuth(p.apiCLIDeploy))
	mux.HandleFunc("/api/deploy", p.protected(p.handleDeploy))
	mux.HandleFunc("/api/logs", p.protected(p.handleLogs))
	mux.HandleFunc("/api/logs/history", p.protected(p.handleLogHistory))
	mux.HandleFunc("/api/down", p.protected(p.handleDown))
	mux.HandleFunc("/api/env", p.protected(p.handleEnv))
	mux.HandleFunc("/api/deploys", p.protected(p.handleDeploys))
	mux.HandleFunc("/api/deploys/log", p.protected(p.handleDeployLog))
	mux.HandleFunc("/api/github/status", p.protected(p.handleGithubStatus))
	mux.HandleFunc("/api/github/create", p.protected(p.handleGithubCreate))
	mux.HandleFunc("/api/github/created", p.protected(p.handleGithubCreated))
	mux.HandleFunc("/api/github/installed", p.protected(p.handleGithubInstalled))
	mux.HandleFunc("/api/github/repos", p.protected(p.handleGithubRepos))
	mux.HandleFunc("/api/github/deploy", p.protected(p.handleGithubDeploy))
	mux.HandleFunc("/api/github/hook", p.handleHook)
	mux.Handle("/", p.spa())
	return logRequests(mux)
}

func (p *Panel) protected(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := p.session(r); !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h(w, r)
	}
}

func (p *Panel) session(r *http.Request) (sess, bool) {
	c, err := r.Cookie("skiff_session")
	if err != nil {
		return sess{}, false
	}
	return getSession(c.Value)
}

// setSession returns an error and sets no cookie if the session can't be stored, so the browser never gets a cookie with no backing row (would look logged-in, then 401).
func (p *Panel) setSession(w http.ResponseWriter, userID, teamID string) error {
	tok := randToken()
	if err := putSession(tok, userID, teamID); err != nil {
		return err
	}
	// SameSite=Strict is the CSRF defense — the browser won't attach the session cross-site, so a crafted link can't trigger a deploy. Secure off only for local http.
	http.SetCookie(w, &http.Cookie{
		Name: "skiff_session", Value: tok, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   p.domain != "" && p.domain != "localhost",
	})
	return nil
}

func (p *Panel) teamID(r *http.Request) string {
	s, _ := p.session(r)
	return s.teamID
}

func (p *Panel) isOwner(r *http.Request) bool {
	s, ok := p.session(r)
	if !ok {
		return false
	}
	role, ok := p.auth.Role(s.userID, s.teamID)
	return ok && role == auth.RoleOwner
}

func (p *Panel) canAccess(r *http.Request, app string) bool {
	src, ok := getSource(app)
	if !ok || src.Team == "" {
		return false
	}
	s, ok := p.session(r)
	if !ok {
		return false
	}
	_, member := p.auth.Role(s.userID, src.Team)
	return member
}

func sanitizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func injectToken(gitURL, token string) string {
	const p = "https://"
	if strings.HasPrefix(gitURL, p) {
		return p + token + "@" + gitURL[len(p):]
	}
	return gitURL
}

func cloneError(b []byte) string {
	var last, fatal string
	for _, ln := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		last = ln
		if strings.Contains(ln, "fatal:") || strings.Contains(ln, "error:") {
			fatal = ln
		}
	}
	msg := fatal
	if msg == "" {
		msg = last
	}
	if strings.Contains(msg, "could not read Username") ||
		strings.Contains(msg, "Authentication failed") ||
		strings.Contains(msg, "terminal prompts disabled") {
		return "repository is private or not found — connect GitHub, add a token, or make it public"
	}
	return msg
}

// guard runs a background-loop iteration, recovering and logging any panic so one bad tick can't take down the control plane.
func guard(name string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("skiff: %s panicked, continuing: %v", name, r)
		}
	}()
	fn()
}

func (p *Panel) prewarmImages(images ...string) {
	for _, img := range images {
		if !p.eng.ImageExists(img) {
			_ = p.eng.PullImage(img)
		}
	}
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func randToken() string { return randHex(18) }
