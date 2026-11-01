package panel

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/nuelScript/skiff/internal/github"
)

// grantGate backs off from a GitHub call the app isn't permitted to make, retrying hourly in case the permission was granted.
type grantGate struct {
	mu         sync.Mutex
	permission string
	needed     bool
	at         time.Time
}

func (g *grantGate) blocked() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.needed && time.Since(g.at) < time.Hour
}

func (g *grantGate) needsGrant() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.needed
}

func (g *grantGate) ok() {
	g.mu.Lock()
	g.needed = false
	g.mu.Unlock()
}

// note records a failed call and reports whether it was something other than a missing permission.
func (g *grantGate) note(err error) bool {
	if err == nil {
		return false
	}
	if !errors.Is(err, github.ErrForbidden) {
		return true
	}
	g.mu.Lock()
	if !g.needed {
		log.Printf("skiff: grant the GitHub App %q", g.permission)
	}
	g.needed, g.at = true, time.Now()
	g.mu.Unlock()
	return false
}

var prGate = &grantGate{permission: "Pull requests: write"}

func previewCommentBody(app, url, commit, status string, at time.Time) string {
	state := map[string]string{
		"building": "🔨 Building",
		"live":     "✅ Ready",
		"failed":   "❌ Failed — see the build log in the dashboard",
		"canceled": "⏹ Superseded by a newer push",
	}[status]
	if state == "" {
		state = status
	}
	if commit == "" {
		commit = "—"
	}
	return fmt.Sprintf("**Preview** · %s\n\n| | |\n|---|---|\n| URL | %s |\n| Commit | `%s` |\n| Project | `%s` |\n| Updated | %s |\n",
		state, url, commit, app, at.UTC().Format("Jan 2, 15:04 UTC"))
}

// commentOnPR keeps one comment on the branch's open pull request in step with its preview deploy.
func (p *Panel) commentOnPR(src Source, commit, status string) {
	if src.Parent == "" || src.Repo == "" || src.Branch == "" {
		return
	}
	gh := github.Load()
	if !gh.Installed() {
		return
	}
	if prGate.blocked() {
		return
	}
	pr, err := gh.OpenPullRequest(src.Repo, src.Branch)
	if err != nil || pr == 0 {
		p.notePRError(err)
		return
	}
	var commentID int64
	_ = sqlDB.QueryRow(`SELECT comment_id FROM pr_comments WHERE app=? AND pr=?`, src.App, pr).Scan(&commentID)
	body := previewCommentBody(src.App, "https://"+src.App+"."+p.domain, commit, status, time.Now())
	id, err := gh.UpsertComment(src.Repo, pr, commentID, body)
	if err != nil {
		p.notePRError(err)
		return
	}
	_, _ = sqlDB.Exec(`INSERT INTO pr_comments(app,pr,comment_id) VALUES(?,?,?)
		ON CONFLICT(app,pr) DO UPDATE SET comment_id=excluded.comment_id`, src.App, pr, id)
	prGate.ok()
}

func (p *Panel) notePRError(err error) {
	if prGate.note(err) {
		log.Printf("skiff: preview comment: %v", err)
	}
}
