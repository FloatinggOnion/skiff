package panel

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nuelScript/skiff/internal/config"
	"github.com/nuelScript/skiff/internal/edge"
	"github.com/nuelScript/skiff/internal/registry"
)

// cloneURLAllowed rejects non-http(s) clone URLs, since git's ext::/file::/ssh transports can execute arbitrary commands during clone (GIT_ALLOW_PROTOCOL is the second line of defense), and hosts on private networks.
func cloneURLAllowed(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return false
	}
	return cloneHostAllowed(u.Hostname())
}

type appView struct {
	Name    string `json:"name"`
	State   string `json:"state"`
	URL     string `json:"url"`
	Repo    string `json:"repo,omitempty"`
	Branch  string `json:"branch,omitempty"`
	Auto    bool   `json:"auto"`
	Commit  string `json:"commit,omitempty"`
	Message string `json:"message,omitempty"`
	Updated int64  `json:"updated,omitempty"`
	Origin  string `json:"origin,omitempty"`
	Image   string `json:"image,omitempty"`
	// Domain is the project's main custom domain, shown in place of its Skiff address.
	Domain  string `json:"domain,omitempty"`
	Private bool   `json:"private,omitempty"`
	// Previews counts its preview environments, which aren't listed as projects themselves.
	Previews int `json:"previews,omitempty"`
}

// displayState reports a sleeping app as asleep rather than as a stopped container.
func (p *Panel) displayState(app, container string) string {
	if isSleeping(app) {
		return "sleeping"
	}
	return p.eng.State(container)
}

func (p *Panel) handleSystem(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"selfDeploy": p.selfRepo != "",
		"repo":       p.selfRepo,
		"branch":     p.selfBranch,
		"deploys":    appDeploys("panel"),
	})
}

func (p *Panel) handleApps(w http.ResponseWriter, r *http.Request) {
	team := p.teamID(r)
	apps, err := registry.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	domains := projectDomains(team)
	out := make([]appView, 0)
	for _, a := range apps {
		src, ok := getSource(a.Name)
		if !ok || src.Team != team {
			continue
		}
		if src.Parent != "" || isPreviewStack(src.Stack) {
			continue
		}
		av := appView{
			Name:   a.Name,
			State:  p.displayState(a.Name, a.Container),
			URL:    "https://" + a.Name + "." + p.domain,
			Repo:   src.Repo,
			Branch: src.Branch,
			Auto:   src.Auto,
			Origin: src.Origin,
			Image:  src.Image,
		}
		if hosts := domains[a.Name]; len(hosts) > 0 {
			av.Domain = hosts[0]
		}
		av.Private = src.Private
		av.Previews = len(previewSources(a.Name))
		if ds := appDeploys(a.Name); len(ds) > 0 {
			av.Commit = ds[0].Commit
			av.Message = ds[0].Message
			av.Updated = ds[0].Started
		}
		out = append(out, av)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

type projectView struct {
	Name string `json:"name"`
	// Parent is the project this one is a preview of.
	Parent      string        `json:"parent,omitempty"`
	State       string        `json:"state"`
	URL         string        `json:"url"`
	Repo        string        `json:"repo"`
	Branch      string        `json:"branch"`
	RootDir     string        `json:"rootDir"`
	Port        string        `json:"port"`
	Auto        bool          `json:"auto"`
	PreviewAuto bool          `json:"previewAuto"`
	Replicas    int           `json:"replicas"`
	Running     int           `json:"running"`
	Release     string        `json:"release"`
	Autoscale   bool          `json:"autoscale"`
	ScaleMin    int           `json:"scaleMin"`
	ScaleMax    int           `json:"scaleMax"`
	ScaleCPU    int           `json:"scaleCpu"`
	Deploys     []Deploy      `json:"deploys"`
	Previews    []previewView `json:"previews"`
	Origin      string        `json:"origin,omitempty"`
	SleepAfter  int           `json:"sleepAfter"`
	Sleeping    bool          `json:"sleeping"`
	HealthPath  string        `json:"healthPath"`
	// HealthTimeout is in seconds; 0 means the default.
	HealthTimeout int `json:"healthTimeout"`
	// PreviewProtected means its previews ask visitors for a password.
	PreviewProtected bool `json:"previewProtected"`
	MaxReplicas      int  `json:"maxReplicas"`
	Private          bool `json:"private"`
	RateLimit        int  `json:"rateLimit"`
	// InternalURL is how other services on the team's network reach it.
	InternalURL string `json:"internalUrl"`
	// SetupNote is the first-run step for a project installed from the catalog.
	SetupNote string `json:"setupNote,omitempty"`
	Memory    string `json:"memory"`
	CPU       string `json:"cpu"`
	// Domains are the project's custom domains, main one first; URL stays its Skiff address.
	Domains []string `json:"domains"`
	// Image is set for a project that runs a prebuilt image instead of building a repo.
	Image string `json:"image,omitempty"`
}

func (p *Panel) handleProject(w http.ResponseWriter, r *http.Request) {
	app := sanitizeName(r.URL.Query().Get("app"))
	if !p.canAccess(r, app) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
		src, _ := getSource(app)
		state := "missing"
		running := 0
		containerPort := 0
		if apps, err := registry.Load(); err == nil {
			if a, ok := apps[app]; ok {
				containerPort = a.Port
				state = p.displayState(app, a.Container)
				running = len(a.Replicas)
				if running == 0 {
					running = 1
				}
			}
		}
		deploys := appDeploys(app)
		p.markRollbackable(app, deploys)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(projectView{
			Name: app, State: state, URL: "https://" + app + "." + p.domain,
			Repo: src.Repo, Branch: src.Branch, RootDir: src.RootDir, Port: src.Port,
			Auto: src.Auto, PreviewAuto: src.PreviewAuto, Replicas: src.Replicas, Running: running,
			Release: src.Release, Autoscale: src.Autoscale, ScaleMin: src.ScaleMin, ScaleMax: src.ScaleMax, ScaleCPU: src.ScaleCPU,
			Deploys: deploys, Previews: p.buildPreviews(app), Origin: src.Origin, Parent: src.Parent,
			SleepAfter: src.SleepAfter, Sleeping: isSleeping(app),
			HealthPath: src.HealthPath, HealthTimeout: src.HealthTimeout,
			PreviewProtected: previewPasswordHash(app) != "",
			Image:            src.Image,
			MaxReplicas:      maxReplicas(),
			Private:          src.Private, RateLimit: src.RateLimit,
			InternalURL: internalURL(app, src, containerPort),
			SetupNote:   catalogNote(src.Catalog),
			Memory:      src.Memory,
			CPU:         src.CPU,
			Domains:     domainsOr(projectDomains(src.Team)[app]),
		})
	case http.MethodPut:
		var body struct {
			Branch        string `json:"branch"`
			RootDir       string `json:"rootDir"`
			Port          string `json:"port"`
			Auto          bool   `json:"auto"`
			PreviewAuto   bool   `json:"previewAuto"`
			Replicas      int    `json:"replicas"`
			Release       string `json:"release"`
			Autoscale     bool   `json:"autoscale"`
			ScaleMin      int    `json:"scaleMin"`
			ScaleMax      int    `json:"scaleMax"`
			ScaleCPU      int    `json:"scaleCpu"`
			SleepAfter    int    `json:"sleepAfter"`
			HealthPath    string `json:"healthPath"`
			HealthTimeout int    `json:"healthTimeout"`
			Image         string `json:"image"`
			Memory        string `json:"memory"`
			CPU           string `json:"cpu"`
			Private       bool   `json:"private"`
			RateLimit     int    `json:"rateLimit"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		src, ok := getSource(app)
		if !ok {
			http.Error(w, "unknown project", http.StatusNotFound)
			return
		}
		body.HealthPath = strings.TrimSpace(body.HealthPath)
		if err := config.ValidateHealth(body.HealthPath, body.HealthTimeout); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		src.HealthPath, src.HealthTimeout = body.HealthPath, body.HealthTimeout
		body.Memory, body.CPU = strings.ToLower(strings.TrimSpace(body.Memory)), strings.TrimSpace(body.CPU)
		if err := config.ValidateResources(body.Memory, body.CPU); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		src.Memory, src.CPU = body.Memory, body.CPU
		if body.RateLimit < 0 || body.RateLimit > maxRateLimit {
			http.Error(w, "rate limit must be between 0 and 1,000,000 requests a minute", http.StatusBadRequest)
			return
		}
		src.Private, src.RateLimit = body.Private, body.RateLimit
		if src.Image != "" {
			// An image project's port may be left empty to use the one the image exposes.
			img, port := strings.TrimSpace(body.Image), strings.TrimSpace(body.Port)
			if err := config.ValidateImage(img); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if !validPort(port) {
				http.Error(w, "port must be a number from 1 to 65535", http.StatusBadRequest)
				return
			}
			src.Image, src.Port = img, port
		} else {
			src.Branch = strings.TrimSpace(body.Branch)
			src.RootDir = strings.TrimSpace(body.RootDir)
			if port := strings.TrimSpace(body.Port); port != "" {
				if !validPort(port) {
					http.Error(w, "port must be a number from 1 to 65535", http.StatusBadRequest)
					return
				}
				src.Port = port
			}
		}
		if body.Replicas >= 1 && body.Replicas <= maxReplicas() {
			src.Replicas = body.Replicas
		}
		src.Release = strings.TrimSpace(body.Release)
		src.Auto = body.Auto
		src.PreviewAuto = body.PreviewAuto
		src.Autoscale = body.Autoscale
		if body.ScaleMin >= 1 && body.ScaleMin <= maxReplicas() {
			src.ScaleMin = body.ScaleMin
		}
		if body.ScaleMax >= 1 && body.ScaleMax <= maxReplicas() {
			src.ScaleMax = body.ScaleMax
		}
		if src.ScaleMax < src.ScaleMin {
			src.ScaleMax = src.ScaleMin
		}
		if body.ScaleCPU >= 10 && body.ScaleCPU <= 100 {
			src.ScaleCPU = body.ScaleCPU
		}
		if body.SleepAfter == 0 || (body.SleepAfter >= 5 && body.SleepAfter <= 24*60) {
			src.SleepAfter = body.SleepAfter
		}
		// Sleep wakes on a public visit, which a private app never gets, so it would stay asleep for good.
		if src.Private {
			src.SleepAfter = 0
		}
		if err := putSource(src); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		_ = writeEdgeFile()
		p.audit(r, "settings.update", app, "")
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

const maxRateLimit = 1_000_000

func edgeFile() string { return edge.DefaultFile() }

var edgeMu sync.Mutex

// writeEdgeFile tells the router each app's rules. A preview follows its project, so a private service's previews aren't public either.
func writeEdgeFile() error {
	edgeMu.Lock()
	defer edgeMu.Unlock()
	sources := allSources()
	byApp := make(map[string]Source, len(sources))
	for _, s := range sources {
		byApp[s.App] = s
	}
	rules := edge.Rules{}
	for _, s := range sources {
		owner := s
		if parent, ok := byApp[s.Parent]; ok && s.Parent != "" {
			owner = parent
		}
		if owner.Private || owner.RateLimit > 0 || s.TCPPort > 0 {
			rules[s.App] = edge.Rule{Private: owner.Private, RateLimit: owner.RateLimit, TCPPort: s.TCPPort}
		}
	}
	return edge.Save(edgeFile(), rules)
}

// internalURL is the app's address on the team's network; its stable name needs a deploy since this feature arrived.
func internalURL(app string, src Source, port int) string {
	if port == 0 {
		port, _ = strconv.Atoi(src.Port)
	}
	if port == 0 {
		return "http://" + app
	}
	return fmt.Sprintf("http://%s:%d", app, port)
}

func domainsOr(hosts []string) []string {
	if hosts == nil {
		return []string{}
	}
	return hosts
}

func (p *Panel) handleRedeploy(w http.ResponseWriter, r *http.Request) {
	app := sanitizeName(r.URL.Query().Get("app"))
	if !p.canAccess(r, app) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	src, ok := getSource(app)
	if !ok {
		http.Error(w, "unknown project", http.StatusNotFound)
		return
	}
	if src.Origin == originCLI {
		http.Error(w, cliOnlyMsg, http.StatusConflict)
		return
	}
	id := newDeployID()
	p.audit(r, "deploy", app, "redeploy")
	go p.runDeploy(src, "", "", "", "redeploy", id)
	p.tailLog(w, r, app, id)
}

func (p *Panel) markRollbackable(app string, deploys []Deploy) {
	retained := map[string]bool{}
	for _, t := range p.eng.AppImageTags(app) {
		retained[t] = true
	}
	current := ""
	for _, d := range deploys {
		if d.Status == "live" {
			current = d.ID
			break
		}
	}
	for i := range deploys {
		deploys[i].Rollbackable = deploys[i].ID != current && retained[deploys[i].ID]
	}
}

func (p *Panel) handleRollback(w http.ResponseWriter, r *http.Request) {
	app := sanitizeName(r.URL.Query().Get("app"))
	target := sanitizeID(r.URL.Query().Get("id"))
	if !p.canAccess(r, app) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	src, ok := getSource(app)
	if !ok {
		http.Error(w, "unknown project", http.StatusNotFound)
		return
	}
	var commit, message string
	for _, d := range appDeploys(app) {
		if d.ID == target {
			commit, message = d.Commit, d.Message
			break
		}
	}
	id := newDeployID()
	detail := ""
	if commit != "" {
		detail = "to " + commit
	}
	p.audit(r, "rollback", app, detail)
	go p.runRollback(src, target, commit, message, id)
	p.tailLog(w, r, app, id)
}

func (p *Panel) handleCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	app := sanitizeName(r.URL.Query().Get("app"))
	id := sanitizeID(r.URL.Query().Get("id"))
	if !p.canAccess(r, app) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if !cancelInflight(app, id) && deployStatus(app, id) == "building" {
		setDeployStatus(app, id, "canceled")
	}
	w.WriteHeader(http.StatusNoContent)
}

func (p *Panel) handleDeploy(w http.ResponseWriter, r *http.Request) {
	git := strings.TrimSpace(r.URL.Query().Get("git"))
	name := sanitizeName(r.URL.Query().Get("name"))
	port := strings.TrimSpace(r.URL.Query().Get("port"))
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	rootDir := strings.TrimSpace(r.URL.Query().Get("rootdir"))
	if git == "" || name == "" {
		http.Error(w, "git url and name are required", http.StatusBadRequest)
		return
	}
	if !cloneURLAllowed(git) {
		http.Error(w, "git url must be a public http(s) URL (for a private git host, set SKIFF_ALLOW_PRIVATE_ENDPOINTS=1 on the server)", http.StatusBadRequest)
		return
	}
	if !validPort(port) {
		http.Error(w, "port must be a number from 1 to 65535", http.StatusBadRequest)
		return
	}
	team := p.teamID(r)
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
	src := Source{App: name, Port: port, CloneURL: git, RootDir: rootDir, Team: team}
	if err := putSource(src); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	envStage.release(name)

	auth := ""
	if token != "" {
		auth = injectToken(git, token)
	}
	id := newDeployID()
	p.audit(r, "deploy", name, "new app from git")
	go p.runDeploy(src, auth, "", "", "manual", id)
	p.tailLog(w, r, name, id)
}

func (p *Panel) handleLogs(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("app")
	if !p.canAccess(r, name) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	apps, err := registry.Load()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	app, ok := apps[name]
	if !ok {
		http.Error(w, "unknown app", http.StatusNotFound)
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	p.followLogs(r.Context(), logTargets(name, app, p.eng.WorkerContainers(name), workerNames(name)), func(line string) {
		fmt.Fprintf(w, "data: %s\n\n", line)
		fl.Flush()
	})
}

func (p *Panel) handleDown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := r.URL.Query().Get("app")
	if !p.canAccess(r, name) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	p.removeApp(name)
	p.audit(r, "project.delete", name, "")
	w.WriteHeader(http.StatusNoContent)
}

// removeApp stops a build in flight and deletes the app with everything it runs; a build left going could otherwise register a container after the project is gone.
func (p *Panel) removeApp(name string) {
	cancelInflight(name, "")
	for deadline := time.Now().Add(20 * time.Second); building(name) && time.Now().Before(deadline); {
		time.Sleep(250 * time.Millisecond)
	}
	// Remove every container — all replicas, not just the representative — or leftovers keep serving (router finds backends by label) and hold host ports.
	for _, c := range p.eng.AppContainers(name) {
		_ = p.eng.Remove(c)
	}
	for _, c := range p.eng.WorkerContainers(name) {
		_ = p.eng.Remove(c)
	}
	_, _ = sqlDB.Exec(`DELETE FROM workers WHERE app=?`, name)
	p.removePreviewDatabases(name)
	if deleteAppDomains(name) {
		writeDomainsFile()
	}
	_, _ = registry.Delete(name)
	p.removeAppVolumes(name)
	appLogs.forget(name)
	p.releaseAttachments(name)
	deleteSource(name)
	p.removeAppImages(name)
}

func (p *Panel) handleEnv(w http.ResponseWriter, r *http.Request) {
	app := sanitizeName(r.URL.Query().Get("app"))
	if _, exists := getSource(app); exists {
		if !p.canAccess(r, app) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	} else {
		// Not deployed yet: reserve the pending name for this team so another team can't read or hijack the env it'll inherit on first deploy.
		team := p.teamID(r)
		if team == "" || !envStage.reserve(app, team, time.Now()) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}
	get, set, action := getEnv, setEnv, "env.update"
	if r.URL.Query().Get("scope") == "preview" {
		if src, ok := getSource(app); !ok || src.Parent != "" {
			http.Error(w, "preview overrides belong to a project", http.StatusNotFound)
			return
		}
		get, set, action = getPreviewEnv, setPreviewEnv, "env.preview.update"
	}
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(get(app))
	case http.MethodPut:
		var body struct {
			Vars []EnvVar `json:"vars"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if err := set(app, body.Vars); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		p.audit(r, action, app, "")
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (p *Panel) handleSharedEnv(w http.ResponseWriter, r *http.Request) {
	team := p.teamID(r)
	if team == "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sharedEnv(team))
	case http.MethodPut:
		var body struct {
			Vars []EnvVar `json:"vars"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if err := setSharedEnv(team, body.Vars); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
