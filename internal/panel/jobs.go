package panel

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

type Job struct {
	ID       string `json:"id"`
	App      string `json:"app"`
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
	LastRun  int64  `json:"lastRun"`
	LastOk   bool   `json:"lastOk"`
	Next     int64  `json:"next"`
	Created  int64  `json:"created"`
}

type jobRow struct {
	ID, App, Team, Name, Schedule, Command string
	LastRun                                int64
	LastOk                                 int
	Created                                int64
}

const jobCols = `id,app,team,name,schedule,command,last_run,last_ok,created`

var (
	jobMu      sync.Mutex
	jobRunning = map[string]bool{}
)

func claimJob(id string) bool {
	jobMu.Lock()
	defer jobMu.Unlock()
	if jobRunning[id] {
		return false
	}
	jobRunning[id] = true
	return true
}

func releaseJob(id string) {
	jobMu.Lock()
	delete(jobRunning, id)
	jobMu.Unlock()
}

func scanJob(row interface{ Scan(...any) error }) (jobRow, bool) {
	var j jobRow
	if row.Scan(&j.ID, &j.App, &j.Team, &j.Name, &j.Schedule, &j.Command, &j.LastRun, &j.LastOk, &j.Created) != nil {
		return jobRow{}, false
	}
	return j, true
}

func getJob(id string) (jobRow, bool) {
	return scanJob(sqlDB.QueryRow(`SELECT `+jobCols+` FROM jobs WHERE id=?`, id))
}

func queryJobs(q string, args ...any) []jobRow {
	rows, err := sqlDB.Query(q, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []jobRow
	for rows.Next() {
		if j, ok := scanJob(rows); ok {
			out = append(out, j)
		}
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

func listJobRows(app string) []jobRow {
	return queryJobs(`SELECT `+jobCols+` FROM jobs WHERE app=? ORDER BY created`, app)
}

func allJobRows() []jobRow { return queryJobs(`SELECT ` + jobCols + ` FROM jobs`) }

func putJob(j jobRow) error {
	_, err := sqlDB.Exec(`INSERT INTO jobs(`+jobCols+`) VALUES(?,?,?,?,?,?,?,?,?)`,
		j.ID, j.App, j.Team, j.Name, j.Schedule, j.Command, j.LastRun, j.LastOk, j.Created)
	return err
}

func setJobRun(id string, when int64, ok bool) {
	_, _ = sqlDB.Exec(`UPDATE jobs SET last_run=?, last_ok=? WHERE id=?`, when, b2i(ok), id)
}

func toJob(j jobRow) Job {
	var next int64
	if s, err := cron.ParseStandard(j.Schedule); err == nil {
		base := time.Now()
		if j.LastRun > 0 {
			base = time.Unix(j.LastRun, 0)
		}
		next = s.Next(base).Unix()
	}
	return Job{
		ID: j.ID, App: j.App, Name: j.Name, Schedule: j.Schedule, Command: j.Command,
		LastRun: j.LastRun, LastOk: j.LastOk != 0, Next: next, Created: j.Created,
	}
}

func (p *Panel) execJob(j jobRow) (string, error) {
	src, ok := getSource(j.App)
	if !ok {
		return "", fmt.Errorf("unknown app")
	}
	image := "skiff-" + j.App + ":latest"
	if !p.eng.ImageExists(image) {
		return "", fmt.Errorf("deploy the app before running jobs")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	out, err := p.eng.RunOnce(ctx, image, j.Command, p.appSettings(src))
	setJobRun(j.ID, time.Now().Unix(), err == nil)
	return out, err
}

func (p *Panel) jobLoop() {
	time.Sleep(90 * time.Second)
	for {
		guard("jobLoop", func() {
			now := time.Now()
			for _, j := range allJobRows() {
				sched, err := cron.ParseStandard(j.Schedule)
				if err != nil {
					continue
				}
				base := time.Unix(j.Created, 0)
				if j.LastRun > 0 {
					base = time.Unix(j.LastRun, 0)
				}
				if sched.Next(base).After(now) {
					continue
				}
				if !claimJob(j.ID) {
					continue
				}
				go func(j jobRow) {
					defer releaseJob(j.ID)
					guard("job:"+j.ID, func() { _, _ = p.execJob(j) })
				}(j)
			}
		})
		time.Sleep(time.Minute)
	}
}

func (p *Panel) handleJobs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		app := sanitizeName(r.URL.Query().Get("app"))
		if !p.canAccess(r, app) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		out := []Job{}
		for _, j := range listJobRows(app) {
			out = append(out, toJob(j))
		}
		writeJSON(w, out)

	case http.MethodPost:
		app := sanitizeName(r.URL.Query().Get("app"))
		if !p.canAccess(r, app) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var body struct{ Name, Schedule, Command string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		j, problem := newJob(app, body.Name, body.Schedule, body.Command)
		if problem != "" {
			http.Error(w, problem, http.StatusBadRequest)
			return
		}
		if err := putJob(j); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		p.audit(r, "job.set", app, j.Name)
		writeJSON(w, toJob(j))

	case http.MethodDelete:
		j, ok := getJob(sanitizeID(r.URL.Query().Get("id")))
		if !ok || !p.canAccess(r, j.App) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		_, _ = sqlDB.Exec(`DELETE FROM jobs WHERE id=?`, j.ID)
		p.audit(r, "job.delete", j.App, j.Name)
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// newJob checks a scheduled job's settings, the same for the dashboard and the API.
func newJob(app, name, schedule, command string) (jobRow, string) {
	schedule, command = strings.TrimSpace(schedule), strings.TrimSpace(command)
	if command == "" {
		return jobRow{}, "a command is required"
	}
	if _, err := cron.ParseStandard(schedule); err != nil {
		return jobRow{}, "invalid schedule — use cron syntax like \"0 3 * * *\""
	}
	src, _ := getSource(app)
	return jobRow{
		ID: randToken()[:12], App: app, Team: src.Team, Name: cmp.Or(strings.TrimSpace(name), "job"),
		Schedule: schedule, Command: command, LastOk: 1, Created: time.Now().Unix(),
	}, ""
}

func (p *Panel) handleJobRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	j, ok := getJob(sanitizeID(r.URL.Query().Get("id")))
	if !ok || !p.canAccess(r, j.App) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if !claimJob(j.ID) {
		http.Error(w, "this job is already running", http.StatusConflict)
		return
	}
	defer releaseJob(j.ID)
	p.audit(r, "job.run", j.App, j.Name)
	out, err := p.execJob(j)
	writeJSON(w, map[string]any{"ok": err == nil, "output": out})
}
