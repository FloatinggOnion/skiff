package panel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/nuelScript/skiff/internal/gate"
	"github.com/nuelScript/skiff/internal/github"
)

// A whole stack per pull request is heavy for one server, so the number that can run at once is capped.
const stackPreviewLimit = 3

var stackPreviewMu sync.Mutex

// stackPreviewPlan marks a stack being created as a preview of another.
type stackPreviewPlan struct{ parent, mode, repo string }

func stackPreviewName(parent, branch string) string {
	slug := slugify(parent + "-" + branch)
	if len(slug) > 40 {
		sum := sha256.Sum256([]byte(branch))
		slug = strings.Trim(slug[:33], "-") + "-" + hex.EncodeToString(sum[:3])
	}
	return slug
}

func stackPreviewNames(parent string) []string {
	rows, err := sqlDB.Query(`SELECT name FROM stacks WHERE parent=? ORDER BY name`, parent)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			out = append(out, n)
		}
	}
	return out
}

func isPreviewStack(name string) bool {
	if name == "" {
		return false
	}
	var parent string
	return sqlDB.QueryRow(`SELECT parent FROM stacks WHERE name=?`, name).Scan(&parent) == nil && parent != ""
}

func stackPreviewsForBranch(repo, branch string) []string {
	rows, err := sqlDB.Query(`SELECT name FROM stacks WHERE parent<>'' AND repo=? AND repo<>'' AND branch=? AND team=?`, repo, branch, foundingTeam())
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			out = append(out, n)
		}
	}
	return out
}

// stacksWantingPreview are the stacks that give this branch's pull request an environment.
func stacksWantingPreview(repo, branch string) []string {
	rows, err := sqlDB.Query(`SELECT name FROM stacks WHERE parent='' AND repo=? AND repo<>'' AND branch<>? AND preview_data<>'' AND team=?`, repo, branch, foundingTeam())
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			out = append(out, n)
		}
	}
	return out
}

func (p *Panel) createStackPreview(rec stackRecord, branch string) (string, error) {
	stackPreviewMu.Lock()
	defer stackPreviewMu.Unlock()
	if rec.Parent != "" {
		return "", errors.New("a preview can't have previews of its own")
	}
	if rec.CloneURL == "" {
		return "", errors.New("previews build a branch of a repository, and this stack came from a pasted file")
	}
	branch = strings.TrimSpace(branch)
	if branch == "" || branch == rec.Branch {
		return "", errors.New("choose a branch other than the stack's own")
	}
	name := stackPreviewName(rec.Name, branch)
	if name == "" {
		return "", errors.New("couldn't make a preview name from that branch")
	}
	if cur, ok := getStackRecord(name); ok {
		if cur.Parent == rec.Name && cur.Branch == branch {
			return name, nil
		}
		return "", fmt.Errorf("the name %s is already in use", name)
	}
	if len(stackPreviewNames(rec.Name)) >= stackPreviewLimit {
		return "", fmt.Errorf("%s already has %d previews, which is the limit. Close a pull request or delete a preview first", rec.Name, stackPreviewLimit)
	}
	if err := hasRoomFor(skiffDir(), 0); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	s, err := loadStack(ctx, stackRequest{Name: name, Git: rec.CloneURL, Branch: branch, Path: rec.Path, Vars: rec.Vars}, rec.Team, false)
	if err != nil {
		return "", err
	}
	if len(s.plan.Problems) > 0 {
		return "", errors.New(s.plan.Problems[0])
	}
	mode := rec.PreviewData
	if mode == "" {
		mode = previewDataEmpty
	}
	if err := p.createStack(s, rec.Team, "", rec.Vars, &stackPreviewPlan{parent: rec.Name, mode: mode, repo: rec.Repo}); err != nil {
		return "", err
	}
	_, _ = sqlDB.Exec(`UPDATE stacks SET preview_error='' WHERE name=?`, rec.Name)
	recordAudit(rec.Team, AuditEntry{Actor: "skiff", Action: "stack.preview", Target: name, Detail: "of " + rec.Name + ", " + mode})
	return name, nil
}

// copyParentData loads the parent's latest backups before any app starts. An error removes the whole preview, because one that started anyway would look fine with the wrong data.
func (p *Panel) copyParentData(ctx context.Context, plan stackPreviewPlan, dbs []dbRow) error {
	if plan.mode != previewDataCopy {
		return nil
	}
	parents := map[string]dbRow{}
	for _, d := range stackDatabases(plan.parent) {
		parents[d.Alias] = d
	}
	for _, d := range dbs {
		source, ok := parents[d.Alias]
		if !ok {
			continue // new in this branch, so there's nothing to copy
		}
		if source.Engine != d.Engine {
			return fmt.Errorf("%s is %s in this branch and %s in the stack", d.Alias, d.Engine, source.Engine)
		}
		if err := p.copyBackupInto(ctx, source, d); err != nil {
			return fmt.Errorf("%s: %w", d.Alias, err)
		}
	}
	return nil
}

func (p *Panel) copyBackupInto(ctx context.Context, source, d dbRow) error {
	b, err := p.latestBackup(source, func(s string) { log.Printf("skiff: stack preview %s: %s", d.Stack, s) })
	if err != nil {
		return err
	}
	path := backupPath(b)
	if err := hasRoomFor(filepath.Dir(path), b.Size*3); err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("reading its backup: %w", err)
	}
	defer f.Close()
	return p.loadBackup(ctx, d, f)
}

func (p *Panel) noteStackPreviewFailure(rec stackRecord, branch string, err error) {
	reason := fmt.Sprintf("The preview of %s couldn't start: %v", branch, err)
	log.Printf("skiff: %s", reason)
	_, _ = sqlDB.Exec(`UPDATE stacks SET preview_error=? WHERE name=?`, reason, rec.Name)
	go dispatchAlert(alertEvent{Team: rec.Team, Kind: "stack.preview", App: rec.Name,
		Title: "A preview of " + rec.Name + " couldn't start", Detail: reason})
}

// removeStack deletes a stack with everything in it, previews first.
func (p *Panel) removeStack(name string) {
	for _, child := range stackPreviewNames(name) {
		p.removeStack(child)
	}
	for _, s := range stackApps(name) {
		p.removeApp(s.App)
	}
	for _, d := range stackDatabases(name) {
		p.removeStackDB(d)
	}
	_ = p.eng.RemoveNetwork(stackNetwork(name))
	_, _ = sqlDB.Exec(`DELETE FROM stacks WHERE name=?`, name)
	_, _ = sqlDB.Exec(`DELETE FROM pr_comments WHERE app=?`, name)
	_ = writeGateFile()
	_ = writeEdgeFile()
}

// onStackPullRequest gives an opened pull request its environment, and takes it away when the pull request closes.
func (p *Panel) onStackPullRequest(pr github.PullRequest) {
	switch pr.Action {
	case "opened", "reopened", "ready_for_review":
		for _, name := range stacksWantingPreview(pr.Repo, pr.Branch) {
			rec, ok := getStackRecord(name)
			if !ok {
				continue
			}
			go guard("stackPreview", func() {
				if _, err := p.createStackPreview(rec, pr.Branch); err != nil {
					p.noteStackPreviewFailure(rec, pr.Branch, err)
				}
			})
		}
	case "closed":
		p.removeBranchStacks(pr.Repo, pr.Branch, "pull request closed")
	}
}

func (p *Panel) removeBranchStacks(repo, branch, why string) {
	for _, name := range stackPreviewsForBranch(repo, branch) {
		if rec, ok := getStackRecord(name); ok {
			recordAudit(rec.Team, AuditEntry{Actor: "push", Action: "stack.delete", Target: name, Detail: why})
		}
		stack := name
		go guard("removeStackPreview", func() { p.removeStack(stack) })
	}
}

func stackPreviewCommentBody(name, commit string, rows []stackPreviewRow, at time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**Stack preview** · `%s`\n\n| Service | Status | Address |\n|---|---|---|\n", name)
	for _, r := range rows {
		state := map[string]string{"building": "🔨 Building", "live": "✅ Ready", "failed": "❌ Failed", "canceled": "⏹ Superseded"}[r.status]
		if state == "" {
			state = r.status
		}
		address := "private"
		if r.url != "" {
			address = r.url
		}
		fmt.Fprintf(&b, "| %s | %s | %s |\n", r.service, state, address)
	}
	if commit == "" {
		commit = "—"
	}
	fmt.Fprintf(&b, "\nCommit `%s` · Updated %s\n", commit, at.UTC().Format("Jan 2, 15:04 UTC"))
	return b.String()
}

type stackPreviewRow struct{ service, status, url string }

// commentOnStackPreview keeps one comment on the branch's open pull request in step with its preview.
func (p *Panel) commentOnStackPreview(name, commit string) {
	rec, ok := getStackRecord(name)
	if !ok || rec.Parent == "" || rec.Repo == "" || !githubUsable(rec.Team) {
		return
	}
	gh := github.Load()
	if !gh.Installed() || prGate.blocked() {
		return
	}
	pr, err := gh.OpenPullRequest(rec.Repo, rec.Branch)
	if err != nil || pr == 0 {
		p.notePRError(err)
		return
	}
	var rows []stackPreviewRow
	for _, s := range stackApps(name) {
		r := stackPreviewRow{service: s.Service, status: "building"}
		if ds := appDeploys(s.App); len(ds) > 0 {
			r.status = ds[0].Status
		}
		if !s.Private {
			r.url = "https://" + s.App + "." + p.domain
		}
		rows = append(rows, r)
	}
	var commentID int64
	_ = sqlDB.QueryRow(`SELECT comment_id FROM pr_comments WHERE app=? AND pr=?`, name, pr).Scan(&commentID)
	id, err := gh.UpsertComment(rec.Repo, pr, commentID, stackPreviewCommentBody(name, commit, rows, time.Now()))
	if err != nil {
		p.notePRError(err)
		return
	}
	_, _ = sqlDB.Exec(`INSERT INTO pr_comments(app,pr,comment_id) VALUES(?,?,?)
		ON CONFLICT(app,pr) DO UPDATE SET comment_id=excluded.comment_id`, name, pr, id)
	prGate.ok()
}

func (p *Panel) handleStackPreviews(w http.ResponseWriter, r *http.Request) {
	team := p.teamID(r)
	if team == "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var body struct {
		Name     string  `json:"name"`
		Branch   string  `json:"branch"`
		Mode     string  `json:"mode"`
		Password *string `json:"password"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body) != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	rec, ok := getStackRecord(sanitizeName(body.Name))
	if !ok || rec.Team != team {
		http.Error(w, "no such stack", http.StatusNotFound)
		return
	}
	switch r.Method {
	case http.MethodPost:
		name, err := p.createStackPreview(rec, body.Branch)
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		p.audit(r, "stack.preview", name, "of "+rec.Name)
		writeJSON(w, map[string]string{"name": name})

	case http.MethodPut:
		if !p.isOwner(r) {
			http.Error(w, "only a team owner can change this", http.StatusForbidden)
			return
		}
		if rec.Parent != "" || rec.CloneURL == "" {
			http.Error(w, "previews need a stack from a repository", http.StatusConflict)
			return
		}
		if body.Mode != "" && body.Mode != previewDataEmpty && body.Mode != previewDataCopy {
			http.Error(w, "unknown mode", http.StatusBadRequest)
			return
		}
		if body.Password != nil {
			hash := ""
			if *body.Password != "" {
				if n := utf8.RuneCountInString(*body.Password); n < 8 || len(*body.Password) > 72 {
					http.Error(w, "use 8 to 72 characters", http.StatusBadRequest)
					return
				}
				var err error
				if hash, err = gate.Hash(*body.Password); err != nil {
					http.Error(w, "couldn't set the password", http.StatusInternalServerError)
					return
				}
			}
			if _, err := sqlDB.Exec(`UPDATE stacks SET preview_password=? WHERE name=?`, hash, rec.Name); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			_ = writeGateFile()
			p.audit(r, "previews.password", rec.Name, "changed")
		}
		if _, err := sqlDB.Exec(`UPDATE stacks SET preview_data=? WHERE name=?`, body.Mode, rec.Name); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		p.audit(r, "stack.preview-data", rec.Name, body.Mode)
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
