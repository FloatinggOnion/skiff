package panel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nuelScript/skiff/internal/compose"
	"github.com/nuelScript/skiff/internal/config"
	"github.com/nuelScript/skiff/internal/docker"
	"github.com/nuelScript/skiff/internal/github"
)

func stackNetwork(stack string) string { return "skiff-s-" + sanitizeName(stack) }

// appNetwork is the network an app's containers join: its stack's when it has one, where services find each other by name.
func appNetwork(src Source) string {
	if src.Stack != "" {
		return stackNetwork(src.Stack)
	}
	return teamNetwork(src.Team)
}

func stackAliases(src Source) []string {
	if src.Stack == "" || src.Service == "" {
		return nil
	}
	return []string{src.Service}
}

func stackCommand(src Source) []string {
	if src.Command == "" {
		return nil
	}
	var c []string
	_ = json.Unmarshal([]byte(src.Command), &c)
	return c
}

func tomlList(items []string) string {
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = strconv.Quote(s)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func (d dbRow) network() string {
	if d.Stack != "" {
		return stackNetwork(d.Stack)
	}
	return teamNetwork(d.Team)
}

func (d dbRow) aliases() []string {
	if d.Alias == "" {
		return nil
	}
	return []string{d.Alias}
}

type stackRequest struct {
	Name    string            `json:"name"`
	Compose string            `json:"compose"`
	Git     string            `json:"git"`
	Token   string            `json:"token"`
	Branch  string            `json:"branch"`
	Path    string            `json:"path"`
	Vars    map[string]string `json:"vars"`
}

// stackPlanView describes a plan without environment values, which often hold secrets.
type stackItemView struct {
	Service  string   `json:"service"`
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	Image    string   `json:"image,omitempty"`
	Builds   bool     `json:"builds,omitempty"`
	Port     int      `json:"port,omitempty"`
	Public   bool     `json:"public,omitempty"`
	Replicas int      `json:"replicas,omitempty"`
	Volumes  []string `json:"volumes,omitempty"`
	EnvKeys  []string `json:"envKeys,omitempty"`
	WorkerOf string   `json:"workerOf,omitempty"`
	Engine   string   `json:"engine,omitempty"`
}

type stackPlanView struct {
	Stack    string          `json:"stack"`
	Items    []stackItemView `json:"items"`
	Problems []string        `json:"problems"`
	Notes    []string        `json:"notes"`
	Unset    []string        `json:"unset"`
}

func viewPlan(p *compose.Plan) stackPlanView {
	v := stackPlanView{Stack: p.Stack, Items: []stackItemView{}, Problems: append([]string{}, p.Problems...),
		Notes: append([]string{}, p.Notes...), Unset: append([]string{}, p.Unset...)}
	for _, it := range p.Items {
		iv := stackItemView{Service: it.Service, Name: it.Name, Kind: string(it.Kind), Image: it.Image, Builds: it.Build != nil,
			Port: it.Port, Public: it.Public, Replicas: it.Replicas, WorkerOf: it.WorkerOf, Engine: it.Engine}
		for _, vol := range it.Volumes {
			iv.Volumes = append(iv.Volumes, vol.Name+" → "+vol.Path)
		}
		if it.Kind != compose.KindDatabase {
			for k := range it.Env {
				iv.EnvKeys = append(iv.EnvKeys, k)
			}
		}
		v.Items = append(v.Items, iv)
	}
	return v
}

// stackSource is what a request's Compose file came from.
type stackSource struct {
	plan     *compose.Plan
	cloneURL string
	branch   string
	path     string
	dir      string // the Compose file's folder within the repo
}

const maxComposeFile = 1 << 20

// loadStack reads the request's Compose file and plans it. Variables come only from the request and the repo's .env, never from the panel's own environment.
func loadStack(ctx context.Context, req stackRequest, team string, updating bool) (*stackSource, error) {
	var name string
	if name = strings.TrimSpace(req.Name); name == "" || sanitizeName(name) != name {
		return nil, errors.New("name must be lowercase letters, digits or dashes")
	}
	out := &stackSource{branch: strings.TrimSpace(req.Branch)}
	var data []byte
	read := func(string) ([]byte, error) { return nil, errors.New("env_file needs a repository") }
	repoEnv := map[string]string{}

	switch {
	case strings.TrimSpace(req.Git) != "":
		git := strings.TrimSpace(req.Git)
		if !cloneURLAllowed(git) {
			return nil, errors.New("git url must be an http(s) URL")
		}
		out.cloneURL = git
		work, err := os.MkdirTemp("", "skiff-stack-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(work)
		auth := git
		if req.Token != "" {
			auth = injectToken(git, req.Token)
		} else if githubRepo(git) != "" {
			if gh := github.Load(); gh.Installed() {
				if u, err := gh.CloneURLWithToken(git); err == nil {
					auth = u
				}
			}
		}
		args := []string{"clone", "--depth", "1"}
		if out.branch != "" {
			args = append(args, "--branch", out.branch)
		}
		cl := exec.CommandContext(ctx, "git", append(args, auth, work)...)
		cl.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ALLOW_PROTOCOL=https:http")
		if b, err := cl.CombinedOutput(); err != nil {
			return nil, errors.New("couldn't clone the repository: " + cloneError(b))
		}
		if out.branch == "" {
			if b, err := exec.CommandContext(ctx, "git", "-C", work, "rev-parse", "--abbrev-ref", "HEAD").Output(); err == nil {
				out.branch = strings.TrimSpace(string(b))
			}
		}
		within := func(p string) string { return filepath.Join(work, filepath.Clean("/"+p)) }
		out.path = strings.TrimSpace(req.Path)
		if out.path == "" {
			for _, n := range compose.FileNames {
				if _, err := os.Stat(within(n)); err == nil {
					out.path = n
					break
				}
			}
			if out.path == "" {
				return nil, errors.New("no Compose file at the top of the repository: give its path")
			}
		}
		if data, err = readLimited(within(out.path)); err != nil {
			return nil, fmt.Errorf("couldn't read %s in the repository", out.path)
		}
		out.dir = filepath.Dir(filepath.Clean("/" + out.path))
		if b, err := readLimited(within(filepath.Join(out.dir, ".env"))); err == nil {
			repoEnv = compose.ParseEnvFile(b)
		}
		read = func(p string) ([]byte, error) { return readLimited(within(filepath.Join(out.dir, p))) }
	case strings.TrimSpace(req.Compose) != "":
		data = []byte(req.Compose)
	default:
		return nil, errors.New("paste a Compose file or give a repository")
	}

	lookup := func(k string) (string, bool) {
		if v, ok := req.Vars[k]; ok {
			return v, true
		}
		v, ok := repoEnv[k]
		return v, ok
	}
	f, err := compose.Parse(data, lookup)
	if err != nil {
		return nil, err
	}
	out.plan = compose.BuildPlan(name, f, read)
	checkStack(out, team, updating)
	return out, nil
}

func readLimited(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxComposeFile))
}

// checkStack adds what only the panel can know: taken names, and limits every deploy path enforces.
func checkStack(s *stackSource, team string, updating bool) {
	p := s.plan
	if t, ok := stackTeam(p.Stack); ok && !updating {
		if t == team {
			p.Problems = append(p.Problems, "a stack with this name already exists")
		} else {
			p.Problems = append(p.Problems, "this name is taken")
		}
	}
	for _, it := range p.Items {
		if it.Kind != compose.KindApp {
			continue
		}
		if existing, taken := getSource(it.Name); taken && (!updating || existing.Stack != p.Stack) {
			p.Problems = append(p.Problems, fmt.Sprintf("%s: an app called %s already exists", it.Service, it.Name))
		}
		if it.Build != nil && s.cloneURL == "" {
			p.Problems = append(p.Problems, it.Service+": builds from source, so this stack needs a repository rather than a pasted file")
		}
		if it.Build != nil {
			if rel := filepath.Clean(filepath.Join(s.dir, it.Build.Context)); strings.HasPrefix(rel, "..") {
				p.Problems = append(p.Problems, it.Service+": its build folder is outside the repository")
			}
		}
		if it.Build == nil && it.Image != "" {
			if err := config.ValidateImage(it.Image); err != nil {
				p.Problems = append(p.Problems, it.Service+": "+err.Error())
			}
		}
		if err := config.ValidateResources(it.Memory, it.CPUs); err != nil {
			p.Problems = append(p.Problems, it.Service+": "+err.Error())
		}
		vols := make([]config.Volume, 0, len(it.Volumes))
		for _, v := range it.Volumes {
			vols = append(vols, config.Volume{Name: v.Name, Path: v.Path})
		}
		if err := config.ValidateVolumes(vols); err != nil {
			p.Problems = append(p.Problems, it.Service+": "+err.Error())
		}
	}
}

func stackTeam(name string) (string, bool) {
	var team string
	if sqlDB.QueryRow(`SELECT team FROM stacks WHERE name=?`, name).Scan(&team) != nil {
		return "", false
	}
	return team, true
}

var githubURL = regexp.MustCompile(`^https?://github\.com/([^/\s]+/[^/\s]+?)(?:\.git)?/?$`)

// githubRepo is the owner/name that push webhooks match on; other hosts have none.
func githubRepo(cloneURL string) string {
	if m := githubURL.FindStringSubmatch(cloneURL); m != nil {
		return m[1]
	}
	return ""
}

// shellJoin rebuilds a command for workers, which run through sh -c.
func shellJoin(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(out, " ")
}

// stackSourceFor turns a planned app into a project.
func stackSourceFor(s *stackSource, team string, it compose.Item) Source {
	src := Source{
		App: it.Name, Team: team, Stack: s.plan.Stack, Service: it.Service, Replicas: it.Replicas,
		Memory: it.Memory, CPU: it.CPUs, Private: !it.Public, HealthPath: it.HealthPath, Port: "3000",
	}
	if it.Port != 0 {
		src.Port = strconv.Itoa(it.Port)
	}
	if it.HealthPath == "" && !it.Public {
		src.HealthMode = "tcp"
		if it.Port == 0 {
			src.HealthMode = "none"
		}
	}
	if len(it.Command) > 0 {
		if b, err := json.Marshal(it.Command); err == nil {
			src.Command = string(b)
		}
	}
	if it.Build != nil {
		src.CloneURL, src.Branch, src.Auto, src.Repo = s.cloneURL, s.branch, true, githubRepo(s.cloneURL)
		src.RootDir = strings.TrimPrefix(filepath.Clean(filepath.Join(s.dir, it.Build.Context)), "/")
		src.Dockerfile = it.Build.Dockerfile
	} else {
		src.Image = it.Image
	}
	return src
}

// provisionStackDB starts a stack's database with the image and credentials its Compose file asked for.
func (p *Panel) provisionStackDB(team, stack string, it compose.Item) (dbRow, error) {
	e := dbEngines[it.Engine]
	id := randToken()[:12]
	container := "skiff-db-" + id
	user, pass, dbname := it.DBUser, it.DBPassword, it.DB
	if user == "" {
		user = e.user
	}
	if pass == "" && it.Engine != "redis" {
		pass = randToken()
	}
	if e.hasDB && dbname == "" {
		dbname = sanitizeID(strings.ReplaceAll(it.Service, "_", "-"))
		dbname = strings.ReplaceAll(dbname, "-", "_")
	}
	env, cmd := e.container(user, pass, dbname)
	net := stackNetwork(stack)
	if err := p.eng.EnsureNetwork(net); err != nil {
		return dbRow{}, err
	}
	volume := container + "-data"
	if _, err := p.eng.RunDatabase(docker.DBRunSpec{
		Name: container, Image: it.Image, Network: net, Aliases: []string{it.Service},
		Volume: volume, MountAt: e.mountAt, Env: env, Cmd: cmd,
		Labels: map[string]string{"skiff.kind": "database", "skiff.db": id, "skiff.team": team, "skiff.stack": stack},
	}); err != nil {
		return dbRow{}, err
	}
	rawCmd, _ := json.Marshal(cmd)
	d := dbRow{
		ID: id, Team: team, Name: it.Name, Engine: it.Engine, Container: container, Host: container, Port: e.port,
		Username: user, Password: pass, DBName: dbname, Created: time.Now().Unix(),
		Image: it.Image, Volume: volume, Cmd: string(rawCmd), Stack: stack, Alias: it.Service,
	}
	return d, putDB(d)
}

// waitDatabaseReady checks over TCP, because the official images run a temporary server while they initialise.
func (p *Panel) waitDatabaseReady(ctx context.Context, d dbRow) error {
	var cmd []string
	switch d.Engine {
	case "postgres":
		cmd = []string{"pg_isready", "-h", "127.0.0.1", "-U", d.Username, "-d", d.DBName}
	case "mysql":
		cmd = []string{"sh", "-c", fmt.Sprintf("MYSQL_PWD='%s' mysql -h 127.0.0.1 --protocol=tcp -u %s -e 'select 1'", d.Password, d.Username)}
	case "mongodb":
		// The temporary server also listens on TCP, but only the final one is the container's main process.
		cmd = []string{"sh", "-c", "grep -qa '^mongod' /proc/1/cmdline && mongosh --quiet --host 127.0.0.1 --eval 'db.runCommand({ping:1})'"}
	case "redis":
		return p.waitRedis(ctx, d)
	default:
		return nil
	}
	var last error
	for {
		if last = p.eng.Exec(ctx, d.Container, cmd, nil, io.Discard); last == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s never became ready: %v", d.Name, last)
		case <-time.After(time.Second):
		}
	}
}

func (p *Panel) removeStackDB(d dbRow) {
	for _, app := range dbAttachments(d.ID) {
		if v, ok := detachDB(d.ID, app); ok {
			deleteEnvVar(app, v)
		}
	}
	_ = p.eng.Stop(d.Container)
	_ = p.eng.Remove(d.Container)
	_ = p.eng.RemoveVolume(d.volume())
	p.removePITR(d)
	removeDatabaseBackups(d.ID)
	deleteDBRow(d.ID)
}

func stackDatabases(stack string) []dbRow {
	rows, err := sqlDB.Query(`SELECT `+dbCols+` FROM databases WHERE stack=?`, stack)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []dbRow
	for rows.Next() {
		if d, ok := scanDB(rows); ok {
			out = append(out, d)
		}
	}
	return out
}

func stackApps(stack string) []Source {
	var out []Source
	for _, s := range allSources() {
		if s.Stack == stack {
			out = append(out, s)
		}
	}
	return out
}

// createStack records everything first and undoes it all if any step fails, so a stack is never half-made.
func (p *Panel) createStack(s *stackSource, team, token string, vars map[string]string, preview *stackPreviewPlan) error {
	plan := s.plan
	var dbs []dbRow
	var apps []Source
	undo := func() {
		for _, a := range apps {
			p.removeApp(a.App)
		}
		for _, d := range dbs {
			p.removeStackDB(d)
		}
		_, _ = sqlDB.Exec(`DELETE FROM stacks WHERE name=?`, plan.Stack)
		_ = p.eng.RemoveNetwork(stackNetwork(plan.Stack))
	}
	sealedVars, err := sealStackVars(vars)
	if err != nil {
		return err
	}
	repo, parent := githubRepo(s.cloneURL), ""
	if preview != nil {
		repo, parent = preview.repo, preview.parent
	}
	if _, err := sqlDB.Exec(`INSERT INTO stacks(name,team,repo,branch,path,clone_url,created,vars,parent) VALUES(?,?,?,?,?,?,?,?,?)`,
		plan.Stack, team, repo, s.branch, s.path, s.cloneURL, time.Now().Unix(), sealedVars, parent); err != nil {
		return err
	}
	for _, it := range plan.Items {
		if it.Kind != compose.KindDatabase {
			continue
		}
		d, err := p.provisionStackDB(team, plan.Stack, it)
		if err != nil {
			undo()
			return fmt.Errorf("starting %s: %w", it.Service, err)
		}
		dbs = append(dbs, d)
	}
	for _, it := range plan.Items {
		if it.Kind != compose.KindApp {
			continue
		}
		src := stackSourceFor(s, team, it)
		if preview != nil && src.CloneURL != "" {
			src.Repo = preview.repo
		}
		if err := putSource(src); err != nil {
			undo()
			return err
		}
		apps = append(apps, src)
		env := make([]EnvVar, 0, len(it.Env))
		for k, v := range it.Env {
			env = append(env, EnvVar{Key: k, Value: v})
		}
		if err := setEnv(src.App, env); err != nil {
			undo()
			return err
		}
		for _, v := range it.Volumes {
			if err := addVolume(src.App, config.Volume{Name: v.Name, Path: v.Path}); err != nil {
				undo()
				return err
			}
		}
	}
	for _, it := range plan.Items {
		if it.Kind != compose.KindWorker {
			continue
		}
		owner := plannedAppName(plan, it.WorkerOf)
		if err := putWorker(workerRow{ID: randToken()[:12], App: owner, Team: team, Name: workerName(it.Service),
			Command: shellJoin(it.Command), Replicas: it.Replicas, Created: time.Now().Unix()}); err != nil {
			undo()
			return err
		}
	}
	auth := ""
	if token != "" && s.cloneURL != "" {
		auth = injectToken(s.cloneURL, token)
	}
	why := stackDeploy{trigger: "manual"}
	if preview != nil {
		// Lock the preview before it has anything to serve.
		_ = writeGateFile()
		_ = writeEdgeFile()
		why.trigger = "preview"
		why.prepare = func(dbs []dbRow) error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			return p.copyParentData(ctx, *preview, dbs)
		}
		why.failed = func(err error) {
			if rec, ok := getStackRecord(preview.parent); ok {
				p.noteStackPreviewFailure(rec, s.branch, err)
			}
			p.removeStack(plan.Stack)
		}
		why.done = func() { p.commentOnStackPreview(plan.Stack, "") }
	}
	go guard("deployStack", func() { p.deployStack(dbs, apps, auth, why) })
	return nil
}

// deployStack waits for databases, then deploys apps one at a time in dependency order.
func (p *Panel) deployStack(dbs []dbRow, apps []Source, auth string, why stackDeploy) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	for _, d := range dbs {
		if err := p.waitDatabaseReady(ctx, d); err != nil {
			log.Printf("skiff: stack database: %v", err)
		}
	}
	if why.prepare != nil {
		if err := why.prepare(dbs); err != nil {
			why.failed(err)
			return
		}
	}
	if why.done != nil {
		why.done()
		defer why.done()
	}
	for _, src := range apps {
		if _, ok := getSource(src.App); !ok {
			continue // deleted while this was waiting its turn
		}
		a := auth
		if src.CloneURL == "" {
			a = ""
		}
		p.runDeploy(src, a, why.commit, why.message, why.trigger, newDeployID())
	}
}

// stackDeploy says why a stack's apps are deploying, for their history.
type stackDeploy struct {
	commit, message, trigger string
	prepare                  func(dbs []dbRow) error // runs once databases are up, before any app
	failed                   func(err error)
	done                     func()
}

type stackMember struct {
	Service string `json:"service"`
	App     string `json:"app"`
	Kind    string `json:"kind"`
	Engine  string `json:"engine,omitempty"`
	State   string `json:"state"`
	Public  bool   `json:"public,omitempty"`
	URL     string `json:"url,omitempty"`
}

type stackView struct {
	Name    string `json:"name"`
	Created int64  `json:"created"`
	FromGit bool   `json:"fromGit,omitempty"`
	// Held explains why the latest push's Compose changes are waiting for someone to apply them.
	Held    string        `json:"held,omitempty"`
	Members []stackMember `json:"members"`
	Branch  string        `json:"branch,omitempty"`
	// PreviewData is what a pull request's preview starts with; empty means pull requests get none.
	PreviewData     string      `json:"previewData"`
	PreviewPassword bool        `json:"previewPassword"`
	PreviewError    string      `json:"previewError,omitempty"`
	PreviewLimit    int         `json:"previewLimit"`
	Previews        []stackView `json:"previews"`
}

func (p *Panel) viewStack(name string, created int64) stackView {
	v := stackView{Name: name, Created: created, Members: []stackMember{}, Previews: []stackView{}, PreviewLimit: stackPreviewLimit}
	if r, ok := getStackRecord(name); ok {
		v.FromGit, v.Held, v.Branch = r.CloneURL != "", r.Held, r.Branch
		v.PreviewData, v.PreviewPassword, v.PreviewError = r.PreviewData, r.PreviewPassword, r.PreviewError
	}
	for _, child := range stackPreviewNames(name) {
		var made int64
		_ = sqlDB.QueryRow(`SELECT created FROM stacks WHERE name=?`, child).Scan(&made)
		v.Previews = append(v.Previews, p.viewStack(child, made))
	}
	for _, d := range stackDatabases(name) {
		v.Members = append(v.Members, stackMember{Service: d.Alias, App: d.Name, Kind: "database", Engine: d.Engine, State: p.eng.State(d.Container)})
	}
	for _, s := range stackApps(name) {
		state := "stopped"
		if len(p.eng.AppContainers(s.App)) > 0 {
			state = "running"
		} else if building(s.App) {
			state = "building"
		}
		m := stackMember{Service: s.Service, App: s.App, Kind: "app", State: state, Public: !s.Private}
		if m.Public {
			m.URL = "https://" + s.App + "." + p.domain
		}
		v.Members = append(v.Members, m)
		for _, w := range listWorkers(s.App) {
			v.Members = append(v.Members, stackMember{Service: w.Name, App: s.App, Kind: "worker", State: state})
		}
	}
	return v
}

func (p *Panel) handleStackPlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	team := p.teamID(r)
	if team == "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var req stackRequest
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2*maxComposeFile)).Decode(&req) != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	s, err := loadStack(ctx, req, team, false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, viewPlan(s.plan))
}

func (p *Panel) handleStacks(w http.ResponseWriter, r *http.Request) {
	team := p.teamID(r)
	if team == "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
		rows, err := sqlDB.Query(`SELECT name, created FROM stacks WHERE team=? AND parent='' ORDER BY created DESC`, team)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		type row struct {
			name    string
			created int64
		}
		var list []row
		for rows.Next() {
			var x row
			if rows.Scan(&x.name, &x.created) == nil {
				list = append(list, x)
			}
		}
		rows.Close()
		out := []stackView{}
		for _, x := range list {
			out = append(out, p.viewStack(x.name, x.created))
		}
		writeJSON(w, out)

	case http.MethodPost:
		var req stackRequest
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2*maxComposeFile)).Decode(&req) != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		s, err := loadStack(ctx, req, team, false)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(s.plan.Problems) > 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(viewPlan(s.plan))
			return
		}
		if err := p.createStack(s, team, req.Token, req.Vars, nil); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		p.audit(r, "stack.create", s.plan.Stack, "")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(viewPlan(s.plan))

	case http.MethodDelete:
		name := sanitizeName(r.URL.Query().Get("name"))
		if t, ok := stackTeam(name); !ok || t != team {
			http.Error(w, "no such stack", http.StatusNotFound)
			return
		}
		p.removeStack(name)
		p.audit(r, "stack.delete", name, "")
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
