package panel

import (
	"encoding/json"
	"net/http"
)

func (p *Panel) apiWorkers(w http.ResponseWriter, r *http.Request) {
	app := sanitizeName(r.PathValue("name"))
	if _, ok := apiSource(r, app); !ok {
		apiErr(w, http.StatusNotFound, "app not found")
		return
	}
	containers := p.eng.WorkerContainers(app)
	out := []Worker{}
	for _, wk := range listWorkers(app) {
		out = append(out, toWorker(wk, containers))
	}
	apiJSON(w, http.StatusOK, out)
}

// apiSetWorker creates the named worker or replaces its command and replicas.
func (p *Panel) apiSetWorker(w http.ResponseWriter, r *http.Request) {
	app := sanitizeName(r.PathValue("name"))
	if _, ok := apiSource(r, app); !ok {
		apiErr(w, http.StatusNotFound, "app not found")
		return
	}
	if workLocked(app) {
		apiErr(w, http.StatusBadRequest, tomlOwned)
		return
	}
	var body struct {
		Command  string `json:"command"`
		Replicas int    `json:"replicas"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body) != nil {
		apiErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	wk, problem := newWorker(app, r.PathValue("worker"), body.Command, body.Replicas)
	if problem != "" {
		apiErr(w, http.StatusBadRequest, problem)
		return
	}
	if err := putWorker(wk); err != nil {
		apiErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	recordAudit(apiTeam(r), AuditEntry{Actor: apiActor(r), Action: "worker.set", Target: app, Detail: wk.Name + " via api"})
	go p.reconcileWorkers(app)
	apiJSON(w, http.StatusOK, toWorker(mustWorker(app, wk.Name, wk), p.eng.WorkerContainers(app)))
}

func (p *Panel) apiRemoveWorker(w http.ResponseWriter, r *http.Request) {
	app := sanitizeName(r.PathValue("name"))
	if _, ok := apiSource(r, app); !ok {
		apiErr(w, http.StatusNotFound, "app not found")
		return
	}
	if workLocked(app) {
		apiErr(w, http.StatusBadRequest, tomlOwned)
		return
	}
	name := sanitizeName(r.PathValue("worker"))
	res, err := sqlDB.Exec(`DELETE FROM workers WHERE app=? AND name=?`, app, name)
	if err != nil {
		apiErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		apiErr(w, http.StatusNotFound, "worker not found")
		return
	}
	recordAudit(apiTeam(r), AuditEntry{Actor: apiActor(r), Action: "worker.delete", Target: app, Detail: name + " via api"})
	go p.reconcileWorkers(app)
	w.WriteHeader(http.StatusNoContent)
}

func (p *Panel) apiJobs(w http.ResponseWriter, r *http.Request) {
	app := sanitizeName(r.PathValue("name"))
	if _, ok := apiSource(r, app); !ok {
		apiErr(w, http.StatusNotFound, "app not found")
		return
	}
	out := []Job{}
	for _, j := range listJobRows(app) {
		out = append(out, toJob(j))
	}
	apiJSON(w, http.StatusOK, out)
}

func (p *Panel) apiCreateJob(w http.ResponseWriter, r *http.Request) {
	app := sanitizeName(r.PathValue("name"))
	if _, ok := apiSource(r, app); !ok {
		apiErr(w, http.StatusNotFound, "app not found")
		return
	}
	if workLocked(app) {
		apiErr(w, http.StatusBadRequest, tomlOwned)
		return
	}
	var body struct {
		Name     string `json:"name"`
		Schedule string `json:"schedule"`
		Command  string `json:"command"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body) != nil {
		apiErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	j, problem := newJob(app, body.Name, body.Schedule, body.Command)
	if problem != "" {
		apiErr(w, http.StatusBadRequest, problem)
		return
	}
	if err := putJob(j); err != nil {
		apiErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	recordAudit(apiTeam(r), AuditEntry{Actor: apiActor(r), Action: "job.set", Target: app, Detail: j.Name + " via api"})
	apiJSON(w, http.StatusCreated, toJob(j))
}

// apiJob finds one of the app's jobs, so a job id from another app or team reads as not found.
func apiJob(r *http.Request) (jobRow, bool) {
	app := sanitizeName(r.PathValue("name"))
	if _, ok := apiSource(r, app); !ok {
		return jobRow{}, false
	}
	j, ok := getJob(sanitizeID(r.PathValue("id")))
	return j, ok && j.App == app
}

func (p *Panel) apiRemoveJob(w http.ResponseWriter, r *http.Request) {
	j, ok := apiJob(r)
	if !ok {
		apiErr(w, http.StatusNotFound, "job not found")
		return
	}
	if workLocked(j.App) {
		apiErr(w, http.StatusBadRequest, tomlOwned)
		return
	}
	_, _ = sqlDB.Exec(`DELETE FROM jobs WHERE id=?`, j.ID)
	recordAudit(apiTeam(r), AuditEntry{Actor: apiActor(r), Action: "job.delete", Target: j.App, Detail: j.Name + " via api"})
	w.WriteHeader(http.StatusNoContent)
}

// apiRunJob runs a job now and waits for it, returning its output.
func (p *Panel) apiRunJob(w http.ResponseWriter, r *http.Request) {
	j, ok := apiJob(r)
	if !ok {
		apiErr(w, http.StatusNotFound, "job not found")
		return
	}
	if !claimJob(j.ID) {
		apiErr(w, http.StatusConflict, "this job is already running")
		return
	}
	defer releaseJob(j.ID)
	recordAudit(apiTeam(r), AuditEntry{Actor: apiActor(r), Action: "job.run", Target: j.App, Detail: j.Name + " via api"})
	out, err := p.execJob(j)
	apiJSON(w, http.StatusOK, map[string]any{"ok": err == nil, "output": out})
}
