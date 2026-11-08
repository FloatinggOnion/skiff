package panel

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
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

var workerLocks sync.Map // app → *sync.Mutex

// reconcileWorkers replaces the app's worker containers with what its rows ask for. A deploy and a change to a worker can both ask at once, and each removes every worker before starting its own, so one runs at a time per app.
func (p *Panel) reconcileWorkers(app string) {
	mu, _ := workerLocks.LoadOrStore(app, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()
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
	settings := p.appSettings(src)
	for _, w := range listWorkers(app) {
		reps := w.Replicas
		if reps < 1 {
			reps = 1
		}
		for range reps {
			name := workerPrefix(app, w.Name) + replicaSuffix()
			_ = p.eng.RunWorker(docker.WorkerSpec{Settings: settings, Name: name, App: app, Image: image, Command: w.Command})
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
		wk, problem := newWorker(app, body.Name, body.Command, body.Replicas)
		if problem != "" {
			http.Error(w, problem, http.StatusBadRequest)
			return
		}
		if err := putWorker(wk); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		p.audit(r, "worker.set", app, wk.Name)
		go p.reconcileWorkers(app)
		// re-read the stored row: on a conflict update the id differs from the one we submitted.
		writeJSON(w, toWorker(mustWorker(app, wk.Name, wk), p.eng.WorkerContainers(app)))

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

// newWorker checks a worker's settings, the same for the dashboard and the API; replicas are kept between 1 and 10.
func newWorker(app, name, command string, replicas int) (workerRow, string) {
	name, command = sanitizeName(name), strings.TrimSpace(command)
	if name == "" {
		return workerRow{}, "a worker name is required"
	}
	if command == "" {
		return workerRow{}, "a command is required"
	}
	src, _ := getSource(app)
	return workerRow{
		ID: randToken()[:12], App: app, Team: src.Team, Name: name,
		Command: command, Replicas: min(max(replicas, 1), 10), Created: time.Now().Unix(),
	}, ""
}

func mustWorker(app, name string, fallback workerRow) workerRow {
	if w, ok := scanWorker(sqlDB.QueryRow(`SELECT `+workerCols+` FROM workers WHERE app=? AND name=?`, app, name)); ok {
		return w
	}
	return fallback
}
