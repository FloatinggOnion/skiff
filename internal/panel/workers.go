package panel

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/nuelScript/skiff/internal/docker"
)

type Worker struct {
	ID       string `json:"id"`
	App      string `json:"app"`
	Name     string `json:"name"`
	Command  string `json:"command"`
	Replicas int    `json:"replicas"`
	Running  int    `json:"running"`
	Created  int64  `json:"created"`
}

type workerRow struct {
	ID, App, Team, Name, Command string
	Replicas                     int
	Created                      int64
}

const workerCols = `id,app,team,name,command,replicas,created`

func scanWorker(row interface{ Scan(...any) error }) (workerRow, bool) {
	var w workerRow
	if row.Scan(&w.ID, &w.App, &w.Team, &w.Name, &w.Command, &w.Replicas, &w.Created) != nil {
		return workerRow{}, false
	}
	return w, true
}

func getWorker(id string) (workerRow, bool) {
	return scanWorker(sqlDB.QueryRow(`SELECT `+workerCols+` FROM workers WHERE id=?`, id))
}

func listWorkers(app string) []workerRow {
	rows, err := sqlDB.Query(`SELECT `+workerCols+` FROM workers WHERE app=? ORDER BY name`, app)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []workerRow
	for rows.Next() {
		if w, ok := scanWorker(rows); ok {
			out = append(out, w)
		}
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

func putWorker(w workerRow) error {
	_, err := sqlDB.Exec(`INSERT INTO workers(`+workerCols+`) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(app,name) DO UPDATE SET command=excluded.command, replicas=excluded.replicas`,
		w.ID, w.App, w.Team, w.Name, w.Command, w.Replicas, w.Created)
	return err
}

func workerPrefix(app, name string) string { return "skiff-" + app + "-" + name + "-" }

func toWorker(w workerRow, containers []string) Worker {
	prefix := workerPrefix(w.App, w.Name)
	running := 0
	for _, c := range containers {
		if strings.HasPrefix(c, prefix) {
			running++
		}
	}
	return Worker{
		ID: w.ID, App: w.App, Name: w.Name, Command: w.Command,
		Replicas: w.Replicas, Running: running, Created: w.Created,
	}
}

func (p *Panel) reconcileWorkers(app string) {
	src, ok := getSource(app)
	if !ok {
		return
	}
	for _, c := range p.eng.WorkerContainers(app) {
		_ = p.eng.Remove(c)
	}
	image := "skiff-" + app + ":latest"
	if !p.eng.ImageExists(image) {
		return
	}
	env, net := p.appRuntime(src)
	_ = p.eng.EnsureNetwork(net)
	mounts := p.appMounts(src)
	for _, w := range listWorkers(app) {
		reps := w.Replicas
		if reps < 1 {
			reps = 1
		}
		for range reps {
			name := workerPrefix(app, w.Name) + replicaSuffix()
			_ = p.eng.RunWorker(docker.WorkerSpec{
				Name: name, App: app, Image: image, Command: w.Command, Env: env, Network: net, Volumes: mounts,
			})
		}
	}
}

func (p *Panel) handleWorkers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		app := sanitizeName(r.URL.Query().Get("app"))
		if !p.canAccess(r, app) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		containers := p.eng.WorkerContainers(app)
		out := []Worker{}
		for _, wk := range listWorkers(app) {
			out = append(out, toWorker(wk, containers))
		}
		writeJSON(w, out)

	case http.MethodPost:
		app := sanitizeName(r.URL.Query().Get("app"))
		if !p.canAccess(r, app) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var body struct {
			Name, Command string
			Replicas      int
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		name := sanitizeName(body.Name)
		command := strings.TrimSpace(body.Command)
		if name == "" {
			http.Error(w, "a worker name is required", http.StatusBadRequest)
			return
		}
		if command == "" {
			http.Error(w, "a command is required", http.StatusBadRequest)
			return
		}
		reps := body.Replicas
		if reps < 1 {
			reps = 1
		}
		if reps > 10 {
			reps = 10
		}
		src, _ := getSource(app)
		wk := workerRow{
			ID: randToken()[:12], App: app, Team: src.Team, Name: name,
			Command: command, Replicas: reps, Created: time.Now().Unix(),
		}
		if err := putWorker(wk); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		p.audit(r, "worker.set", app, name)
		go p.reconcileWorkers(app)
		// re-read the stored row: on a conflict update the id differs from the one we submitted.
		writeJSON(w, toWorker(mustWorker(app, name, wk), p.eng.WorkerContainers(app)))

	case http.MethodDelete:
		wk, ok := getWorker(sanitizeID(r.URL.Query().Get("id")))
		if !ok || !p.canAccess(r, wk.App) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		_, _ = sqlDB.Exec(`DELETE FROM workers WHERE id=?`, wk.ID)
		p.audit(r, "worker.delete", wk.App, wk.Name)
		go p.reconcileWorkers(wk.App)
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func mustWorker(app, name string, fallback workerRow) workerRow {
	if w, ok := scanWorker(sqlDB.QueryRow(`SELECT `+workerCols+` FROM workers WHERE app=? AND name=?`, app, name)); ok {
		return w
	}
	return fallback
}
