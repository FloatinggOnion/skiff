// Package workload is the one definition behind everything Skiff runs for a team: an app, its workers and its scheduled jobs. Each answers the same six questions, so a setting means the same thing whichever kind it's on.
package workload

import "github.com/nuelScript/skiff/internal/config"

type Kind string

const (
	KindApp    Kind = "app"
	KindWorker Kind = "worker"
	KindJob    Kind = "job"
)

type Spec struct {
	Kind Kind `json:"kind"`
	// ID is the app's name for an app, and the row's id for a worker or job.
	ID string `json:"id"`
	// App is the app a worker or job belongs to; an app belongs to itself.
	App     string `json:"app"`
	Name    string `json:"name"`
	Team    string `json:"team"`
	Created int64  `json:"created"`

	Run    Run    `json:"run"`
	When   When   `json:"when"`
	Reach  Reach  `json:"reach"`
	Use    Use    `json:"use"`
	Touch  Touch  `json:"touch"`
	Origin Origin `json:"origin"`
}

// Run is what runs. A worker or job runs its app's latest build, so it only sets Shell.
type Run struct {
	Repo       string `json:"repo,omitempty"`
	Branch     string `json:"branch,omitempty"`
	RootDir    string `json:"rootDir,omitempty"`
	Dockerfile string `json:"dockerfile,omitempty"`
	CloneURL   string `json:"cloneUrl,omitempty"`
	Image      string `json:"image,omitempty"`
	// Command replaces the image's default command, as a JSON list.
	Command string `json:"command,omitempty"`
	// Shell is a command run with sh -c.
	Shell string `json:"shell,omitempty"`
	// Release runs once before a new version goes live, such as migrations.
	Release string `json:"release,omitempty"`
	// MountedFiles are repository files built into the image, as a JSON list.
	MountedFiles string `json:"mountedFiles,omitempty"`
}

// When is when it runs: always, with replicas, or on a schedule.
type When struct {
	Schedule   string `json:"schedule,omitempty"`
	Replicas   int    `json:"replicas,omitempty"`
	Autoscale  bool   `json:"autoscale,omitempty"`
	ScaleMin   int    `json:"scaleMin,omitempty"`
	ScaleMax   int    `json:"scaleMax,omitempty"`
	ScaleCPU   int    `json:"scaleCpu,omitempty"`
	SleepAfter int    `json:"sleepAfter,omitempty"` // minutes without visitors before it stops
	AutoDeploy bool   `json:"autoDeploy,omitempty"` // a push deploys it
}

// Reach is how it's reached. A worker or job isn't.
type Reach struct {
	Port          int    `json:"port,omitempty"`
	Private       bool   `json:"private,omitempty"`
	RateLimit     int    `json:"rateLimit,omitempty"` // requests per minute per visitor
	TCPPort       int    `json:"tcpPort,omitempty"`   // a public port forwarded straight to it
	HealthPath    string `json:"healthPath,omitempty"`
	HealthTimeout int    `json:"healthTimeout,omitempty"` // seconds
	HealthMode    string `json:"healthMode,omitempty"`
}

// Use is what it can use. Empty leaves it to the server-wide safety cap.
type Use struct {
	Memory string `json:"memory,omitempty"`
	CPU    string `json:"cpu,omitempty"`
}

// Touch is what it can touch beyond its environment: how its container starts.
type Touch struct {
	Options config.RunOptions `json:"options,omitzero"`
}

// Origin is where it came from, which decides who may change it.
type Origin struct {
	CLI         bool   `json:"cli,omitempty"` // deployed with skiff deploy, so skiff.toml holds its settings
	Catalog     string `json:"catalog,omitempty"`
	Stack       string `json:"stack,omitempty"`
	Service     string `json:"service,omitempty"`
	Parent      string `json:"parent,omitempty"` // the project this is a preview of
	PreviewAuto bool   `json:"previewAuto,omitempty"`
	PreviewData string `json:"previewData,omitempty"`
}
