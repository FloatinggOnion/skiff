package panel

import (
	"encoding/json"
	"net/http"
	"strings"
)

// deployFilter narrows a project's deployments list. Empty fields match everything.
type deployFilter struct {
	env, status, branch, search string
}

// projectDeploys is a project's deployments and its previews', newest first.
func projectDeploys(project string, f deployFilter, before int64, beforeID string, limit int) []Deploy {
	apps := []string{}
	if f.env != "preview" {
		apps = append(apps, project)
	}
	if f.env != "production" {
		for _, pv := range previewSources(project) {
			apps = append(apps, pv.App)
		}
	}
	out := []Deploy{}
	if len(apps) == 0 {
		return out
	}
	q := `SELECT ` + deployCols + ` FROM deploys WHERE app IN (?` + strings.Repeat(",?", len(apps)-1) + `)`
	args := make([]any, 0, len(apps)+7)
	for _, a := range apps {
		args = append(args, a)
	}
	if f.status != "" {
		q += ` AND status=?`
		args = append(args, f.status)
	}
	if f.branch != "" {
		q += ` AND branch=?`
		args = append(args, f.branch)
	}
	if f.search != "" {
		q += ` AND (message LIKE ? ESCAPE '!' OR commit_sha LIKE ? ESCAPE '!')`
		like := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(f.search) + "%"
		args = append(args, like, like)
	}
	if before > 0 {
		q += ` AND (started < ? OR (started = ? AND id < ?))`
		args = append(args, before, before, beforeID)
	}
	q += ` ORDER BY started DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := sqlDB.Query(q, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		if d, err := scanDeploy(rows); err == nil {
			if d.App != project {
				d.Parent = project
			}
			out = append(out, d)
		}
	}
	if rows.Err() != nil {
		return []Deploy{}
	}
	return out
}

// currentDeploy is the deploy an app is serving: its newest that went live.
func currentDeploy(app string) string {
	var id string
	_ = sqlDB.QueryRow(`SELECT id FROM deploys WHERE app=? AND status='live' ORDER BY started DESC, id DESC LIMIT 1`, app).Scan(&id)
	return id
}

// deployBranches lists the branches a project and its previews have deployed, for the list's filter.
func deployBranches(project string) []string {
	out := []string{}
	rows, err := sqlDB.Query(`SELECT DISTINCT branch FROM deploys WHERE branch<>'' AND (app=? OR app IN (SELECT app FROM sources WHERE parent=?)) ORDER BY branch`, project, project)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var b string
		if rows.Scan(&b) == nil {
			out = append(out, b)
		}
	}
	return out
}

func (p *Panel) handleDeployBranches(w http.ResponseWriter, r *http.Request) {
	app := sanitizeName(r.URL.Query().Get("app"))
	if !p.canViewDeploys(r, app) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	writeJSON(w, deployBranches(app))
}

// handleDeployOne is one deployment, for its own page.
func (p *Panel) handleDeployOne(w http.ResponseWriter, r *http.Request) {
	app := sanitizeName(r.URL.Query().Get("app"))
	id := sanitizeID(r.URL.Query().Get("id"))
	if !p.canViewDeploys(r, app) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	d, err := scanDeploy(sqlDB.QueryRow(`SELECT `+deployCols+` FROM deploys WHERE app=? AND id=?`, app, id))
	if err != nil {
		http.Error(w, "that deployment doesn't exist", http.StatusNotFound)
		return
	}
	deploys := []Deploy{d}
	p.markRollbackable(app, deploys)
	src, _ := getSource(app)
	deploys[0].Parent = src.Parent
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Deploy
		URL    string `json:"url"`
		Origin string `json:"origin,omitempty"`
	}{deploys[0], "https://" + app + "." + p.domain, src.Origin})
}
