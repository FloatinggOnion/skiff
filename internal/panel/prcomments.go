package panel

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/nuelScript/skiff/internal/github"
)

var (
	prMu           sync.Mutex
	prNeedsGrant   bool // the GitHub App hasn't been given "Pull requests: write"
	prNeedsGrantAt time.Time
)

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
	prMu.Lock()
	blocked := prNeedsGrant && time.Since(prNeedsGrantAt) < time.Hour
	prMu.Unlock()
	if blocked {
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
	prMu.Lock()
	prNeedsGrant = false
	prMu.Unlock()
}

func (p *Panel) notePRError(err error) {
	if err == nil {
		return
	}
	if errors.Is(err, github.ErrForbidden) {
		prMu.Lock()
		if !prNeedsGrant {
			log.Printf("skiff: can't comment on pull requests — grant the GitHub App \"Pull requests: write\"")
		}
		prNeedsGrant, prNeedsGrantAt = true, time.Now()
		prMu.Unlock()
		return
	}
	log.Printf("skiff: preview comment: %v", err)
}

func prCommentsNeedGrant() bool {
	prMu.Lock()
	defer prMu.Unlock()
	return prNeedsGrant
}
