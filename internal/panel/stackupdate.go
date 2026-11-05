package panel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nuelScript/skiff/internal/compose"
	"github.com/nuelScript/skiff/internal/config"
	"github.com/nuelScript/skiff/internal/github"
	"github.com/nuelScript/skiff/internal/seal"
)

type stackRecord struct {
	Name, Team, Repo, Branch, Path, CloneURL, Held string
	Parent, PreviewData, PreviewError              string
	PreviewPassword                                bool
	Vars                                           map[string]string
}

func getStackRecord(name string) (stackRecord, bool) {
	r := stackRecord{Name: name, Vars: map[string]string{}}
	var sealed, hash string
	if sqlDB.QueryRow(`SELECT team,repo,branch,path,clone_url,vars,held,parent,preview_data,preview_error,preview_password FROM stacks WHERE name=?`, name).
		Scan(&r.Team, &r.Repo, &r.Branch, &r.Path, &r.CloneURL, &sealed, &r.Held, &r.Parent, &r.PreviewData, &r.PreviewError, &hash) != nil {
		return stackRecord{}, false
	}
	r.PreviewPassword = hash != ""
	if seal.OpenAll(&sealed) == nil && sealed != "" {
		_ = json.Unmarshal([]byte(sealed), &r.Vars)
	}
	return r, true
}

func sealStackVars(vars map[string]string) (string, error) {
	if len(vars) == 0 {
		return "", nil
	}
	b, err := json.Marshal(vars)
	if err != nil {
		return "", err
	}
	return seal.Seal(string(b))
}

// StackChange is one service's difference from what's running. Details name settings and environment keys, never their values.
type StackChange struct {
	Service string   `json:"service"`
	Kind    string   `json:"kind"`
	Action  string   `json:"action"` // add, update or remove
	Details []string `json:"details,omitempty"`
	// DeletesData marks a change that destroys a database or a volume's files.
	DeletesData bool `json:"deletesData,omitempty"`
}

// currentStack is what a stack is running now, read once so comparing it needs no docker or database access.
type currentStack struct {
	Apps    map[string]Source
	Env     map[string]map[string]string
	Volumes map[string][]config.Volume
	Workers map[string]workerRow // by service name
	DBs     map[string]dbRow     // by service name
}

func readCurrentStack(stack string) currentStack {
	c := currentStack{Apps: map[string]Source{}, Env: map[string]map[string]string{}, Volumes: map[string][]config.Volume{},
		Workers: map[string]workerRow{}, DBs: map[string]dbRow{}}
	for _, s := range stackApps(stack) {
		c.Apps[s.Service] = s
		env := map[string]string{}
		for _, e := range getEnv(s.App) {
			env[e.Key] = e.Value
		}
		c.Env[s.Service] = env
		c.Volumes[s.Service] = listVolumes(s.App)
		for _, w := range listWorkers(s.App) {
			c.Workers[w.Name] = w
		}
	}
	for _, d := range stackDatabases(stack) {
		c.DBs[d.Alias] = d
	}
	return c
}

func workerName(service string) string { return sanitizeName(strings.ReplaceAll(service, "_", "-")) }

func stackVolumes(it compose.Item) []config.Volume {
	out := make([]config.Volume, 0, len(it.Volumes))
	for _, v := range it.Volumes {
		out = append(out, config.Volume{Name: v.Name, Path: v.Path})
	}
	return out
}

// diffStack compares a plan with what's running. Problems are changes Skiff refuses to make.
func diffStack(s *stackSource, team string, cur currentStack) (changes []StackChange, problems []string) {
	seen := map[string]bool{}
	for _, it := range s.plan.Items {
		seen[string(it.Kind)+"/"+it.Service] = true
		switch it.Kind {
		case compose.KindDatabase:
			if d, ok := cur.DBs[it.Service]; ok {
				problems = append(problems, databaseChangeProblems(it, d)...)
			} else {
				changes = append(changes, StackChange{Service: it.Service, Kind: "database", Action: "add"})
			}
		case compose.KindWorker:
			w, ok := cur.Workers[workerName(it.Service)]
			if !ok {
				changes = append(changes, StackChange{Service: it.Service, Kind: "worker", Action: "add"})
				continue
			}
			var details []string
			if w.Command != shellJoin(it.Command) {
				details = append(details, "command")
			}
			if w.Replicas != it.Replicas {
				details = append(details, fmt.Sprintf("replicas: %d → %d", w.Replicas, it.Replicas))
			}
			if w.App != plannedAppName(s.plan, it.WorkerOf) {
				details = append(details, "moves to "+it.WorkerOf)
			}
			if len(details) > 0 {
				changes = append(changes, StackChange{Service: it.Service, Kind: "worker", Action: "update", Details: details})
			}
		case compose.KindApp:
			src, ok := cur.Apps[it.Service]
			if !ok {
				changes = append(changes, StackChange{Service: it.Service, Kind: "app", Action: "add"})
				continue
			}
			details, deletes := appChanges(s, team, it, src, cur)
			if len(details) > 0 {
				changes = append(changes, StackChange{Service: it.Service, Kind: "app", Action: "update", Details: details, DeletesData: deletes})
			}
		}
	}
	for svc := range cur.Apps {
		if !seen["app/"+svc] {
			changes = append(changes, StackChange{Service: svc, Kind: "app", Action: "remove", DeletesData: len(cur.Volumes[svc]) > 0})
		}
	}
	for svc := range cur.Workers {
		if !workerPlanned(s.plan, svc) {
			changes = append(changes, StackChange{Service: svc, Kind: "worker", Action: "remove"})
		}
	}
	for svc := range cur.DBs {
		if !seen["database/"+svc] {
			changes = append(changes, StackChange{Service: svc, Kind: "database", Action: "remove", DeletesData: true})
		}
	}
	slices.SortFunc(changes, func(a, b StackChange) int { return strings.Compare(a.Service+a.Action, b.Service+b.Action) })
	slices.Sort(problems)
	return changes, problems
}

func plannedAppName(p *compose.Plan, service string) string {
	for _, i := range p.Items {
		if i.Service == service {
			return i.Name
		}
	}
	return ""
}

func workerPlanned(p *compose.Plan, name string) bool {
	return slices.ContainsFunc(p.Items, func(i compose.Item) bool { return i.Kind == compose.KindWorker && workerName(i.Service) == name })
}

func databaseChangeProblems(it compose.Item, d dbRow) []string {
	var out []string
	const why = "isn't applied to existing data, so Skiff won't change it here. Keep the old value in the file, or move the data with a backup and a new database"
	if it.Image != d.Image {
		out = append(out, fmt.Sprintf("%s: changing the image from %s to %s %s", it.Service, d.Image, it.Image, why))
	}
	if it.DBUser != "" && it.DBUser != d.Username {
		out = append(out, fmt.Sprintf("%s: changing the username %s", it.Service, why))
	}
	if it.DBPassword != "" && it.DBPassword != d.Password {
		out = append(out, fmt.Sprintf("%s: changing the password %s", it.Service, why))
	}
	if it.DB != "" && it.DB != d.DBName {
		out = append(out, fmt.Sprintf("%s: changing the database name %s", it.Service, why))
	}
	return out
}

func appChanges(s *stackSource, team string, it compose.Item, src Source, cur currentStack) (details []string, deletes bool) {
	want := stackSourceFor(s, team, it)
	field := func(label, from, to string) {
		if from != to {
			details = append(details, fmt.Sprintf("%s: %s → %s", label, orNone(from), orNone(to)))
		}
	}
	field("image", src.Image, want.Image)
	field("build folder", src.RootDir, want.RootDir)
	field("dockerfile", src.Dockerfile, want.Dockerfile)
	if src.MountedFiles != want.MountedFiles {
		details = append(details, "files from the repository")
	}
	field("port", src.Port, want.Port)
	field("memory", src.Memory, want.Memory)
	field("cpu", src.CPU, want.CPU)
	field("health check", src.HealthPath, want.HealthPath)
	field("replicas", strconv.Itoa(src.Replicas), strconv.Itoa(want.Replicas))
	if src.Command != want.Command {
		details = append(details, "command")
	}
	if src.Private != want.Private {
		if want.Private {
			details = append(details, "becomes private")
		} else {
			details = append(details, "becomes public")
		}
	}
	var added, changed, removed []string
	for k, v := range it.Env {
		if old, ok := cur.Env[it.Service][k]; !ok {
			added = append(added, k)
		} else if old != v {
			changed = append(changed, k)
		}
	}
	for k := range cur.Env[it.Service] {
		if _, ok := it.Env[k]; !ok {
			removed = append(removed, k)
		}
	}
	for _, g := range []struct {
		label string
		keys  []string
	}{{"adds env", added}, {"changes env", changed}, {"removes env", removed}} {
		if len(g.keys) > 0 {
			slices.Sort(g.keys)
			details = append(details, g.label+": "+strings.Join(g.keys, ", "))
		}
	}
	wantVols := stackVolumes(it)
	for _, v := range wantVols {
		found := slices.IndexFunc(cur.Volumes[it.Service], func(o config.Volume) bool { return o.Name == v.Name })
		switch {
		case found < 0:
			details = append(details, "adds volume "+v.Name)
		case cur.Volumes[it.Service][found].Path != v.Path:
			details = append(details, fmt.Sprintf("moves volume %s to %s", v.Name, v.Path))
		}
	}
	for _, o := range cur.Volumes[it.Service] {
		if !slices.ContainsFunc(wantVols, func(v config.Volume) bool { return v.Name == o.Name }) {
			details = append(details, "deletes volume "+o.Name+" and its files")
			deletes = true
		}
	}
	return details, deletes
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

type stackChangesView struct {
	Stack       string        `json:"stack"`
	FromGit     bool          `json:"fromGit"`
	Changes     []StackChange `json:"changes"`
	Problems    []string      `json:"problems"`
	Notes       []string      `json:"notes"`
	Unset       []string      `json:"unset"`
	DeletesData bool          `json:"deletesData"`
}

type stackUpdateRequest struct {
	stackRequest
	// AllowDataLoss must be true for an update that deletes a database or a volume's files.
	AllowDataLoss bool `json:"allowDataLoss"`
}

// loadStackUpdate reads the stack's Compose file again: from its repository, or from the pasted copy in the request.
func loadStackUpdate(ctx context.Context, req stackRequest, team string) (*stackSource, stackRecord, stackChangesView, error) {
	rec, ok := getStackRecord(req.Name)
	if !ok || rec.Team != team {
		return nil, rec, stackChangesView{}, errors.New("no such stack")
	}
	merged := map[string]string{}
	for k, v := range rec.Vars {
		merged[k] = v
	}
	for k, v := range req.Vars {
		merged[k] = v
	}
	req.Vars = merged
	if rec.CloneURL != "" {
		req.Git, req.Branch, req.Path, req.Compose = rec.CloneURL, rec.Branch, rec.Path, ""
	} else if strings.TrimSpace(req.Compose) == "" {
		return nil, rec, stackChangesView{}, errors.New("paste the updated Compose file")
	}
	s, err := loadStack(ctx, req, team, true)
	if err != nil {
		return nil, rec, stackChangesView{}, err
	}
	if rec.CloneURL != "" {
		s.path = rec.Path
	}
	changes, problems := diffStack(s, team, readCurrentStack(s.plan.Stack))
	s.plan.Problems = append(s.plan.Problems, problems...)
	view := stackChangesView{Stack: s.plan.Stack, FromGit: rec.CloneURL != "", Changes: changes, Problems: append([]string{}, s.plan.Problems...),
		Notes: append([]string{}, s.plan.Notes...), Unset: append([]string{}, s.plan.Unset...)}
	if view.Changes == nil {
		view.Changes = []StackChange{}
	}
	view.DeletesData = slices.ContainsFunc(changes, func(c StackChange) bool { return c.DeletesData })
	return s, rec, view, nil
}

func (p *Panel) handleStackChanges(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	team := p.teamID(r)
	var req stackRequest
	if team == "" || json.NewDecoder(http.MaxBytesReader(w, r.Body, 2*maxComposeFile)).Decode(&req) != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	_, _, view, err := loadStackUpdate(ctx, req, team)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, view)
}

func (p *Panel) handleStackUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	team := p.teamID(r)
	var req stackUpdateRequest
	if team == "" || json.NewDecoder(http.MaxBytesReader(w, r.Body, 2*maxComposeFile)).Decode(&req) != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	s, rec, view, err := loadStackUpdate(ctx, req.stackRequest, team)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(view.Problems) > 0 || (view.DeletesData && !req.AllowDataLoss) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(view)
		return
	}
	merged := rec.Vars
	for k, v := range req.Vars {
		merged[k] = v
	}
	if err := p.applyStackChanges(s, team, req.Token, merged, view.Changes, nil, stackDeploy{trigger: "manual"}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	p.audit(r, "stack.update", s.plan.Stack, fmt.Sprintf("%d change(s)", len(view.Changes)))
	writeJSON(w, view)
}

// applyStackChanges adds first and removes last, so a failure partway leaves everything that was running in place. Only services that changed are redeployed.
func (p *Panel) applyStackChanges(s *stackSource, team, token string, vars map[string]string, changes []StackChange, alsoDeploy []Source, why stackDeploy) error {
	stack := s.plan.Stack
	sealedVars, err := sealStackVars(vars)
	if err != nil {
		return err
	}
	if _, err := sqlDB.Exec(`UPDATE stacks SET vars=?, held='' WHERE name=?`, sealedVars, stack); err != nil {
		return err
	}
	action := map[string]string{}
	for _, c := range changes {
		action[c.Kind+"/"+c.Service] = c.Action
	}
	cur := readCurrentStack(stack)
	var newDBs []dbRow
	for _, it := range s.plan.Items {
		if it.Kind == compose.KindDatabase && action["database/"+it.Service] == "add" {
			d, err := p.provisionStackDB(team, stack, it)
			if err != nil {
				return fmt.Errorf("starting %s: %w", it.Service, err)
			}
			newDBs = append(newDBs, d)
		}
	}
	var order []Source
	for _, it := range s.plan.Items {
		if it.Kind != compose.KindApp {
			continue
		}
		act := action["app/"+it.Service]
		if act == "" {
			continue
		}
		want := stackSourceFor(s, team, it)
		if old, ok := cur.Apps[it.Service]; ok {
			want.RateLimit, want.SleepAfter, want.PreviewAuto, want.Release = old.RateLimit, old.SleepAfter, old.PreviewAuto, old.Release
			want.Autoscale, want.ScaleMin, want.ScaleMax, want.ScaleCPU = old.Autoscale, old.ScaleMin, old.ScaleMax, old.ScaleCPU
		}
		if err := putSource(want); err != nil {
			return err
		}
		env := make([]EnvVar, 0, len(it.Env))
		for k, v := range it.Env {
			env = append(env, EnvVar{Key: k, Value: v})
		}
		if err := setEnv(want.App, env); err != nil {
			return err
		}
		wantVols := stackVolumes(it)
		for _, o := range cur.Volumes[it.Service] {
			if !slices.Contains(wantVols, o) {
				deleteVolume(want.App, o.Name)
			}
		}
		for _, v := range wantVols {
			if !slices.Contains(cur.Volumes[it.Service], v) {
				if err := addVolume(want.App, v); err != nil {
					return err
				}
			}
		}
		order = append(order, want)
	}

	for _, src := range alsoDeploy {
		if !slices.ContainsFunc(order, func(o Source) bool { return o.App == src.App }) {
			order = append(order, src)
		}
	}

	workersChanged := map[string]bool{}
	for _, it := range s.plan.Items {
		if it.Kind != compose.KindWorker || action["worker/"+it.Service] == "" {
			continue
		}
		name := workerName(it.Service)
		owner := plannedAppName(s.plan, it.WorkerOf)
		if old, ok := cur.Workers[name]; ok && old.App != owner {
			_, _ = sqlDB.Exec(`DELETE FROM workers WHERE app=? AND name=?`, old.App, name)
			workersChanged[old.App] = true
		}
		if err := putWorker(workerRow{ID: randToken()[:12], App: owner, Team: team, Name: name,
			Command: shellJoin(it.Command), Replicas: it.Replicas, Created: time.Now().Unix()}); err != nil {
			return err
		}
		workersChanged[owner] = true
	}
	for name, w := range cur.Workers {
		if action["worker/"+name] == "remove" {
			_, _ = sqlDB.Exec(`DELETE FROM workers WHERE app=? AND name=?`, w.App, name)
			workersChanged[w.App] = true
		}
	}

	auth := ""
	if token != "" && s.cloneURL != "" {
		auth = injectToken(s.cloneURL, token)
	}
	removeApps := []string{}
	for svc, src := range cur.Apps {
		if action["app/"+svc] == "remove" {
			removeApps = append(removeApps, src.App)
		}
	}
	removeDBs := []dbRow{}
	for svc, d := range cur.DBs {
		if action["database/"+svc] == "remove" {
			removeDBs = append(removeDBs, d)
		}
	}
	go guard("updateStack", func() {
		p.deployStack(newDBs, order, auth, why)
		for app := range workersChanged {
			if !slices.ContainsFunc(order, func(s Source) bool { return s.App == app }) {
				p.reconcileWorkers(app)
			}
		}
		for _, app := range removeApps {
			p.removeApp(app)
		}
		for _, d := range removeDBs {
			p.removeStackDB(d)
		}
	})
	return nil
}

func stacksForPush(repo, branch string) []string {
	rows, err := sqlDB.Query(`SELECT name FROM stacks WHERE repo=? AND repo<>'' AND (branch='' OR branch=?) AND team=?`, repo, branch, foundingTeam())
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

// holdReason says why a push's Compose changes shouldn't apply on their own. A commit must never delete data or break a service unattended.
func holdReason(view stackChangesView, err error) string {
	switch {
	case err != nil:
		return "Skiff couldn't read the Compose file: " + err.Error()
	case len(view.Problems) > 0:
		return view.Problems[0]
	case view.DeletesData:
		var names []string
		for _, c := range view.Changes {
			if c.DeletesData {
				names = append(names, c.Service)
			}
		}
		return "This would delete data in " + strings.Join(names, ", ") + ", so it needs your confirmation."
	}
	return ""
}

// syncStackOnPush applies safe Compose changes from a push and deploys apps whose code changed. Changes it won't make alone wait for someone to apply them, while the new code still deploys with the current settings.
func (p *Panel) syncStackOnPush(name string, push github.Push) {
	rec, ok := getStackRecord(name)
	if !ok {
		return
	}
	defer p.commentOnStackPreview(name, shortCommit(push.Commit))
	var built []Source
	for _, src := range stackApps(name) {
		if src.Auto && stackAppTouched(src, push.Paths) {
			built = append(built, src)
		}
	}
	why := stackDeploy{commit: push.Commit, message: push.Message, trigger: "push"}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	s, loaded, view, err := loadStackUpdate(ctx, stackRequest{Name: name}, rec.Team)
	if reason := holdReason(view, err); reason != "" {
		_, _ = sqlDB.Exec(`UPDATE stacks SET held=? WHERE name=?`, reason, name)
		recordAudit(rec.Team, AuditEntry{Actor: "push", Action: "stack.held", Target: name, Detail: shortCommit(push.Commit)})
		go dispatchAlert(alertEvent{Team: rec.Team, Kind: "stack.held", App: name,
			Title:  "Changes to " + name + " are waiting for you",
			Detail: reason})
		p.deployStack(nil, built, "", why)
		return
	}
	recordAudit(rec.Team, AuditEntry{Actor: "push", Action: "stack.update", Target: name, Detail: fmt.Sprintf("%s: %d change(s)", shortCommit(push.Commit), len(view.Changes))})
	if err := p.applyStackChanges(s, rec.Team, "", loaded.Vars, view.Changes, built, why); err != nil {
		_, _ = sqlDB.Exec(`UPDATE stacks SET held=? WHERE name=?`, "Applying the latest push failed: "+err.Error(), name)
	}
}
