package panel

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/nuelScript/skiff/internal/config"
	"github.com/nuelScript/skiff/internal/workload"
)

// sourceSpec describes an app in the one workload definition.
func sourceSpec(s Source) workload.Spec {
	port, _ := strconv.Atoi(s.Port)
	return workload.Spec{
		Kind: workload.KindApp, ID: s.App, App: s.App, Name: s.App, Team: s.Team,
		Run: workload.Run{
			Repo: s.Repo, Branch: s.Branch, RootDir: s.RootDir, Dockerfile: s.Dockerfile, CloneURL: s.CloneURL,
			Image: s.Image, Command: s.Command, Release: s.Release, MountedFiles: s.MountedFiles,
		},
		When: workload.When{
			Replicas: s.Replicas, Autoscale: s.Autoscale, ScaleMin: s.ScaleMin, ScaleMax: s.ScaleMax, ScaleCPU: s.ScaleCPU,
			SleepAfter: s.SleepAfter, AutoDeploy: s.Auto,
		},
		Reach: workload.Reach{
			Port: port, Private: s.Private, RateLimit: s.RateLimit, TCPPort: s.TCPPort,
			HealthPath: s.HealthPath, HealthTimeout: s.HealthTimeout, HealthMode: s.HealthMode,
		},
		Use:   workload.Use{Memory: s.Memory, CPU: s.CPU},
		Touch: workload.Touch{Options: config.DecodeRunOptions(s.RunOptions)},
		Origin: workload.Origin{
			CLI: s.Origin == originCLI, Catalog: s.Catalog, Stack: s.Stack, Service: s.Service,
			Parent: s.Parent, PreviewAuto: s.PreviewAuto, PreviewData: s.PreviewData,
		},
	}
}

// specSource is the app row a spec is stored as until the workload table replaces it.
func specSource(w workload.Spec) Source {
	s := Source{
		App: w.ID, Team: w.Team,
		Repo: w.Run.Repo, Branch: w.Run.Branch, RootDir: w.Run.RootDir, Dockerfile: w.Run.Dockerfile, CloneURL: w.Run.CloneURL,
		Image: w.Run.Image, Command: w.Run.Command, Release: w.Run.Release, MountedFiles: w.Run.MountedFiles,
		Replicas: w.When.Replicas, Autoscale: w.When.Autoscale, ScaleMin: w.When.ScaleMin, ScaleMax: w.When.ScaleMax,
		ScaleCPU: w.When.ScaleCPU, SleepAfter: w.When.SleepAfter, Auto: w.When.AutoDeploy,
		Private: w.Reach.Private, RateLimit: w.Reach.RateLimit, TCPPort: w.Reach.TCPPort,
		HealthPath: w.Reach.HealthPath, HealthTimeout: w.Reach.HealthTimeout, HealthMode: w.Reach.HealthMode,
		Memory: w.Use.Memory, CPU: w.Use.CPU,
		Catalog: w.Origin.Catalog, Stack: w.Origin.Stack, Service: w.Origin.Service,
		Parent: w.Origin.Parent, PreviewAuto: w.Origin.PreviewAuto, PreviewData: w.Origin.PreviewData,
	}
	if w.Reach.Port != 0 {
		s.Port = strconv.Itoa(w.Reach.Port)
	}
	if w.Origin.CLI {
		s.Origin = originCLI
	}
	if !w.Touch.Options.IsZero() {
		s.RunOptions = config.EncodeRunOptions(w.Touch.Options)
	}
	return s
}

func workerSpec(r workerRow) workload.Spec {
	return workload.Spec{
		Kind: workload.KindWorker, ID: r.ID, App: r.App, Name: r.Name, Team: r.Team, Created: r.Created,
		Run:  workload.Run{Shell: r.Command},
		When: workload.When{Replicas: r.Replicas},
	}
}

func specWorker(w workload.Spec) workerRow {
	return workerRow{ID: w.ID, App: w.App, Team: w.Team, Name: w.Name, Command: w.Run.Shell, Replicas: w.When.Replicas, Created: w.Created}
}

// jobSpec leaves out when the job last ran, which is what happened rather than what was asked for.
func jobSpec(r jobRow) workload.Spec {
	return workload.Spec{
		Kind: workload.KindJob, ID: r.ID, App: r.App, Name: r.Name, Team: r.Team, Created: r.Created,
		Run:  workload.Run{Shell: r.Command},
		When: workload.When{Schedule: r.Schedule},
	}
}

func specJob(w workload.Spec) jobRow {
	return jobRow{ID: w.ID, App: w.App, Team: w.Team, Name: w.Name, Schedule: w.When.Schedule, Command: w.Run.Shell, Created: w.Created}
}

// apiWorkloads lists everything the team runs, apps, previews, workers and jobs alike, in one shape.
func (p *Panel) apiWorkloads(w http.ResponseWriter, r *http.Request) {
	out := []workload.Spec{}
	sources := allSources()
	slices.SortFunc(sources, func(a, b Source) int { return strings.Compare(a.App, b.App) })
	for _, s := range sources {
		if s.Team != apiTeam(r) {
			continue
		}
		spec := sourceSpec(s)
		spec.Run.CloneURL = "" // it can carry an access token
		out = append(out, spec)
		for _, wk := range listWorkers(s.App) {
			out = append(out, workerSpec(wk))
		}
		for _, j := range listJobRows(s.App) {
			out = append(out, jobSpec(j))
		}
	}
	apiJSON(w, http.StatusOK, out)
}
