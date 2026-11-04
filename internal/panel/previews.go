package panel

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/nuelScript/skiff/internal/github"
	"github.com/nuelScript/skiff/internal/registry"
)

func previewName(app, branch string) string {
	slug := slugify(app + "-" + branch)
	if len(slug) > 50 {
		slug = strings.Trim(slug[:50], "-")
	}
	return slug
}

func slugify(s string) string {
	var b strings.Builder
	dash := true
	for _, r := range strings.ToLower(s) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			dash = false
		case !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimRight(b.String(), "-")
}

func previewSources(parent string) []Source {
	rows, err := sqlDB.Query(`SELECT `+sourceCols+` FROM sources WHERE parent=? ORDER BY app`, parent)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []Source{}
	for rows.Next() {
		if s, ok := scanSource(rows); ok {
			out = append(out, s)
		}
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

// branchPreviews are the previews built from one branch of a repo.
func branchPreviews(repo, branch string) []Source {
	rows, err := sqlDB.Query(`SELECT `+sourceCols+` FROM sources WHERE parent<>'' AND repo=? AND branch=? AND team=?`, repo, branch, foundingTeam())
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Source
	for rows.Next() {
		if s, ok := scanSource(rows); ok {
			out = append(out, s)
		}
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

// previewsToCreate are the projects that get a new preview from this push: automatic previews on, a branch other than their own, no preview yet, and a change inside their root directory.
func previewsToCreate(push github.Push) []Source {
	var out []Source
	for _, app := range productionAppsForRepo(push.Repo) {
		if !app.PreviewAuto || app.Branch == "" || app.Branch == push.Branch {
			continue
		}
		if _, exists := getSource(previewName(app.App, push.Branch)); exists {
			continue
		}
		if !touched(push.Paths, app.RootDir) {
			continue
		}
		out = append(out, app)
	}
	return out
}

type previewView struct {
	Name    string `json:"name"`
	Branch  string `json:"branch"`
	URL     string `json:"url"`
	State   string `json:"state"`
	Status  string `json:"status"`
	Updated int64  `json:"updated"`
}

func (p *Panel) buildPreviews(parent string) []previewView {
	srcs := previewSources(parent)
	out := make([]previewView, 0, len(srcs))
	apps, _ := registry.Load()
	for _, s := range srcs {
		pv := previewView{
			Name:   s.App,
			Branch: s.Branch,
			URL:    "https://" + s.App + "." + p.domain,
			State:  "missing",
		}
		if a, ok := apps[s.App]; ok {
			pv.State = p.eng.State(a.Container)
		}
		if ds := appDeploys(s.App); len(ds) > 0 {
			pv.Status = ds[0].Status
			pv.Updated = ds[0].Started
		}
		out = append(out, pv)
	}
	return out
}

func (p *Panel) handleCreatePreview(w http.ResponseWriter, r *http.Request) {
	parent := sanitizeName(r.URL.Query().Get("app"))
	branch := strings.TrimSpace(r.URL.Query().Get("branch"))
	if !p.canAccess(r, parent) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	src, ok := getSource(parent)
	if !ok || src.Parent != "" {
		http.Error(w, "unknown project", http.StatusNotFound)
		return
	}
	if src.Origin == originCLI {
		http.Error(w, cliOnlyMsg, http.StatusConflict)
		return
	}
	if src.Stack != "" {
		http.Error(w, "A stack's services get previews together: turn them on under Stacks", http.StatusConflict)
		return
	}
	if src.Image != "" {
		http.Error(w, "previews deploy a branch of a repository; this project runs a prebuilt image", http.StatusConflict)
		return
	}
	if branch == "" {
		http.Error(w, "a branch is required", http.StatusBadRequest)
		return
	}
	name, id, ok := p.createPreview(src, branch, "", fmt.Sprintf("preview of %s", branch), p.deployActor(r))
	if !ok {
		http.Error(w, "couldn't make a preview name from that branch: it's empty or clashes with another app", http.StatusBadRequest)
		return
	}
	p.tailLog(w, r, name, id)
}

func (p *Panel) createPreview(parent Source, branch, commit, message, actor string) (name, id string, ok bool) {
	name = previewName(parent.App, branch)
	if name == "" || name == parent.App {
		return "", "", false
	}
	// A preview's name is derived from the branch, so it can land on someone else's app; only this project's own preview may be reused.
	if existing, taken := getSource(name); taken && (existing.Parent != parent.App || existing.Team != parent.Team) {
		return "", "", false
	}
	// auto so a push to that branch redeploys the preview through the webhook path.
	pv := Source{
		App: name, Team: parent.Team, Repo: parent.Repo, Branch: branch,
		RootDir: parent.RootDir, Port: parent.Port, CloneURL: parent.CloneURL,
		Auto: true, Parent: parent.App,
	}
	if putSource(pv) != nil {
		return "", "", false
	}
	// Lock the preview before it has anything to serve.
	_ = writeGateFile()
	_ = writeEdgeFile()
	if rebindBranchDomains(parent.App, branch, name) {
		writeDomainsFile()
	}
	id = newDeployID()
	pv.actor = actor
	go p.runDeploy(pv, "", commit, message, "preview", id)
	return name, id, true
}

func (p *Panel) removeAppImages(app string) {
	for _, t := range p.eng.AppImageTags(app) {
		_ = p.eng.RemoveImage(fmt.Sprintf("skiff-%s:%s", app, t))
	}
	_ = p.eng.RemoveImage("skiff-" + app + ":latest")
}
