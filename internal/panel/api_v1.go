package panel

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nuelScript/skiff/internal/config"
	"github.com/nuelScript/skiff/internal/registry"
)

type ctxKey int

const (
	ctxTeam ctxKey = iota
	ctxActor
)

func bearerToken(r *http.Request) string {
	if after, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return strings.TrimSpace(after)
	}
	return ""
}

func (p *Panel) apiAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		team, name, ok := resolveToken(bearerToken(r))
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="skiff"`)
			apiErr(w, http.StatusUnauthorized, "invalid or missing API token")
			return
		}
		ctx := context.WithValue(r.Context(), ctxTeam, team)
		ctx = context.WithValue(ctx, ctxActor, "token:"+name)
		h(w, r.WithContext(ctx))
	}
}

func apiTeam(r *http.Request) string  { t, _ := r.Context().Value(ctxTeam).(string); return t }
func apiActor(r *http.Request) string { a, _ := r.Context().Value(ctxActor).(string); return a }

func apiJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func apiErr(w http.ResponseWriter, code int, msg string) {
	apiJSON(w, code, map[string]string{"error": msg})
}

func apiSource(r *http.Request, name string) (Source, bool) {
	src, ok := getSource(name)
	if !ok || src.Team != apiTeam(r) {
		return Source{}, false
	}
	return src, true
}

type apiApp struct {
	Name      string `json:"name"`
	State     string `json:"state"`
	URL       string `json:"url"`
	Repo      string `json:"repo,omitempty"`
	Branch    string `json:"branch,omitempty"`
	Image     string `json:"image,omitempty"`
	Replicas  int    `json:"replicas"`
	Running   int    `json:"running"`
	Autoscale bool   `json:"autoscale"`
	Commit    string `json:"commit,omitempty"`
	Updated   int64  `json:"updated,omitempty"`
}

func (p *Panel) apiAppView(a registry.App, src Source) apiApp {
	v := apiApp{
		Name: a.Name, State: p.displayState(a.Name, a.Container),
		URL:  "https://" + a.Name + "." + p.domain,
		Repo: src.Repo, Branch: src.Branch, Image: src.Image,
		Replicas: src.Replicas, Running: len(a.Replicas), Autoscale: src.Autoscale,
	}
	if v.Running == 0 {
		v.Running = 1
	}
	if ds := appDeploys(a.Name); len(ds) > 0 {
		v.Commit, v.Updated = ds[0].Commit, ds[0].Started
	}
	return v
}

func (p *Panel) apiListApps(w http.ResponseWriter, r *http.Request) {
	team := apiTeam(r)
	apps, err := registry.List()
	if err != nil {
		apiErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := []apiApp{}
	for _, a := range apps {
		src, ok := getSource(a.Name)
		if !ok || src.Team != team || src.Parent != "" {
			continue
		}
		out = append(out, p.apiAppView(a, src))
	}
	apiJSON(w, http.StatusOK, out)
}

func (p *Panel) apiGetApp(w http.ResponseWriter, r *http.Request) {
	name := sanitizeName(r.PathValue("name"))
	src, ok := apiSource(r, name)
	if !ok {
		apiErr(w, http.StatusNotFound, "app not found")
		return
	}
	apps, _ := registry.Load()
	apiJSON(w, http.StatusOK, p.apiAppView(apps[name], src))
}

func (p *Panel) apiDeploy(w http.ResponseWriter, r *http.Request) {
	name := sanitizeName(r.PathValue("name"))
	src, ok := apiSource(r, name)
	if !ok {
		apiErr(w, http.StatusNotFound, "app not found")
		return
	}
	if src.Origin == originCLI {
		apiErr(w, http.StatusConflict, cliOnlyMsg)
		return
	}
	// An image project can be pointed at a new tag in the same call, e.g. from CI after pushing it.
	var body struct {
		Image string `json:"image"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body); err != nil && err != io.EOF {
			apiErr(w, http.StatusBadRequest, "body must be JSON, e.g. {\"image\": \"ghcr.io/you/app:v2\"}")
			return
		}
	}
	if img := strings.TrimSpace(body.Image); img != "" {
		if src.Image == "" {
			apiErr(w, http.StatusConflict, "this app builds from a repository; image only applies to apps that run a prebuilt image")
			return
		}
		if err := config.ValidateImage(img); err != nil {
			apiErr(w, http.StatusBadRequest, err.Error())
			return
		}
		src.Image = img
		if err := putSource(src); err != nil {
			apiErr(w, http.StatusInternalServerError, "couldn't save the image")
			return
		}
	}
	id := newDeployID()
	recordAudit(apiTeam(r), AuditEntry{Actor: apiActor(r), Action: "deploy", Target: name, Detail: "via api"})
	src.actor = strings.TrimPrefix(apiActor(r), "token:")
	go p.runDeploy(src, "", "", "", "api", id)
	apiJSON(w, http.StatusAccepted, map[string]string{"id": id, "app": name, "status": "building", "url": p.apiAppURL(src)})
}

func (p *Panel) apiAppURL(src Source) string {
	if src.Private {
		return ""
	}
	return "https://" + src.App + "." + p.domain
}

// apiCreatePreview starts or redeploys a branch's preview, so a pipeline can test a change against a real environment.
func (p *Panel) apiCreatePreview(w http.ResponseWriter, r *http.Request) {
	src, ok := apiSource(r, sanitizeName(r.PathValue("name")))
	if !ok || src.Parent != "" {
		apiErr(w, http.StatusNotFound, "app not found")
		return
	}
	if src.Origin == originCLI {
		apiErr(w, http.StatusConflict, cliOnlyMsg)
		return
	}
	if src.Stack != "" {
		apiErr(w, http.StatusConflict, "A stack's services get previews together: turn them on under Stacks")
		return
	}
	if src.Image != "" {
		apiErr(w, http.StatusConflict, "previews deploy a branch of a repository; this app runs a prebuilt image")
		return
	}
	var body struct {
		Branch string `json:"branch"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body) != nil || strings.TrimSpace(body.Branch) == "" {
		apiErr(w, http.StatusBadRequest, "body must name a branch, e.g. {\"branch\": \"feat/login\"}")
		return
	}
	branch := strings.TrimSpace(body.Branch)
	name, id, ok := p.createPreview(src, branch, "", "preview of "+branch, strings.TrimPrefix(apiActor(r), "token:"))
	if !ok {
		apiErr(w, http.StatusBadRequest, "couldn't make a preview name from that branch: it's empty or clashes with another app")
		return
	}
	recordAudit(apiTeam(r), AuditEntry{Actor: apiActor(r), Action: "preview.create", Target: name, Detail: "via api"})
	apiJSON(w, http.StatusAccepted, map[string]string{"id": id, "app": name, "status": "building", "url": "https://" + name + "." + p.domain})
}

// apiRemovePreview deletes a branch's preview. The branch comes from the query string because branch names contain slashes.
func (p *Panel) apiRemovePreview(w http.ResponseWriter, r *http.Request) {
	parent, ok := apiSource(r, sanitizeName(r.PathValue("name")))
	if !ok {
		apiErr(w, http.StatusNotFound, "app not found")
		return
	}
	name := previewName(parent.App, strings.TrimSpace(r.URL.Query().Get("branch")))
	pv, ok := getSource(name)
	if !ok || pv.Parent != parent.App {
		apiErr(w, http.StatusNotFound, "no preview for that branch")
		return
	}
	recordAudit(apiTeam(r), AuditEntry{Actor: apiActor(r), Action: "project.delete", Target: name, Detail: "via api"})
	p.removeApp(name)
	w.WriteHeader(http.StatusNoContent)
}

func (p *Panel) apiDeployStatus(w http.ResponseWriter, r *http.Request) {
	d, ok := getDeployByID(sanitizeID(r.PathValue("id")))
	if !ok {
		apiErr(w, http.StatusNotFound, "deploy not found")
		return
	}
	if src, ok := getSource(d.App); !ok || src.Team != apiTeam(r) {
		apiErr(w, http.StatusNotFound, "deploy not found")
		return
	}
	apiJSON(w, http.StatusOK, d)
}

func (p *Panel) apiEnv(w http.ResponseWriter, r *http.Request) {
	name := sanitizeName(r.PathValue("name"))
	if _, ok := apiSource(r, name); !ok {
		apiErr(w, http.StatusNotFound, "app not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		apiJSON(w, http.StatusOK, getEnv(name))
	case http.MethodPut:
		var body struct {
			Vars []EnvVar `json:"vars"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			apiErr(w, http.StatusBadRequest, "invalid body")
			return
		}
		if err := setEnv(name, body.Vars); err != nil {
			apiErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		recordAudit(apiTeam(r), AuditEntry{Actor: apiActor(r), Action: "env.update", Target: name, Detail: "via api"})
		w.WriteHeader(http.StatusNoContent)
	}
}

func (p *Panel) apiDeploys(w http.ResponseWriter, r *http.Request) {
	name := sanitizeName(r.PathValue("name"))
	if _, ok := apiSource(r, name); !ok {
		apiErr(w, http.StatusNotFound, "app not found")
		return
	}
	deploys := appDeploys(name)
	p.markRollbackable(name, deploys)
	apiJSON(w, http.StatusOK, deploys)
}

// apiDeployLog returns the last lines of a deploy's build log.
func (p *Panel) apiDeployLog(w http.ResponseWriter, r *http.Request) {
	d, ok := getDeployByID(sanitizeID(r.PathValue("id")))
	if !ok {
		apiErr(w, http.StatusNotFound, "deploy not found")
		return
	}
	if _, ok := apiSource(r, d.App); !ok {
		apiErr(w, http.StatusNotFound, "deploy not found")
		return
	}
	b, _ := os.ReadFile(logPath(d.App, d.ID))
	apiJSON(w, http.StatusOK, map[string]any{"deploy": d, "log": tailLines(string(b), 200)})
}

func (p *Panel) apiLogs(w http.ResponseWriter, r *http.Request) {
	name := sanitizeName(r.PathValue("name"))
	if _, ok := apiSource(r, name); !ok {
		apiErr(w, http.StatusNotFound, "app not found")
		return
	}
	apps, _ := registry.Load()
	a, ok := apps[name]
	if !ok {
		apiErr(w, http.StatusNotFound, "app isn't running")
		return
	}
	lines := 200
	if n, err := strconv.Atoi(r.URL.Query().Get("lines")); err == nil && n > 0 && n <= 2000 {
		lines = n
	}
	targets := logTargets(name, a, p.eng.WorkerContainers(name), workerNames(name))
	if r.URL.Query().Get("follow") == "1" {
		fl, ok := w.(http.Flusher)
		if !ok {
			apiErr(w, http.StatusInternalServerError, "streaming unsupported")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fl.Flush()
		p.followLogs(r.Context(), targets, func(line string) {
			fmt.Fprintln(w, line)
			fl.Flush()
		})
		return
	}
	var all strings.Builder
	for _, t := range targets {
		var buf bytes.Buffer
		_ = p.eng.Logs(t.container, false, strconv.Itoa(lines), &buf)
		sc := bufio.NewScanner(&buf)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			all.WriteString(t.prefix() + sc.Text() + "\n")
		}
	}
	apiJSON(w, http.StatusOK, map[string]string{"app": name, "logs": all.String()})
}

func (p *Panel) apiRollback(w http.ResponseWriter, r *http.Request) {
	name := sanitizeName(r.PathValue("name"))
	src, ok := apiSource(r, name)
	if !ok {
		apiErr(w, http.StatusNotFound, "app not found")
		return
	}
	var body struct {
		Deploy string `json:"deploy"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	target := sanitizeID(body.Deploy)
	deploys := appDeploys(name)
	p.markRollbackable(name, deploys)
	var to *Deploy
	for i := range deploys {
		if deploys[i].ID == target && deploys[i].Rollbackable {
			to = &deploys[i]
		}
	}
	if to == nil {
		apiErr(w, http.StatusBadRequest, "that deploy can't be rolled back to; list deploys and pick one marked rollbackable")
		return
	}
	id := newDeployID()
	recordAudit(apiTeam(r), AuditEntry{Actor: apiActor(r), Action: "rollback", Target: name, Detail: "to " + to.Commit + " via api"})
	go p.runRollback(src, to.ID, to.Commit, to.Message, id)
	apiJSON(w, http.StatusAccepted, map[string]string{"id": id, "app": name, "status": "building"})
}

func (p *Panel) apiInsights(w http.ResponseWriter, r *http.Request) {
	name := sanitizeName(r.PathValue("name"))
	if _, ok := apiSource(r, name); !ok {
		apiErr(w, http.StatusNotFound, "app not found")
		return
	}
	in, err := buildInsights(name, r.URL.Query().Get("range"), time.Now())
	if err != nil {
		apiErr(w, http.StatusInternalServerError, "couldn't read requests")
		return
	}
	up := buildUptime(name, time.Now())
	in.Series = nil
	apiJSON(w, http.StatusOK, map[string]any{
		"traffic": in,
		"uptime":  map[string]any{"up": up.Up, "pct24h": up.Pct24h, "pct7d": up.Pct7d, "incidents": up.Incidents},
	})
}

func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
