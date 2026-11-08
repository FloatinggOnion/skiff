// Package config loads and validates a skiff.toml describing a single app.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/robfig/cron/v3"
)

const DefaultFile = "skiff.toml"

type Config struct {
	Name     string `toml:"name"`
	Replicas int    `toml:"replicas"`

	Server    Server            `toml:"server"`
	Build     Build             `toml:"build"`
	Deploy    Deploy            `toml:"deploy"`
	Resources Resources         `toml:"resources"`
	Env       map[string]string `toml:"env"`
	Secrets   map[string]string `toml:"secrets"` // runtime only, never baked into the image
	Volumes   []Volume          `toml:"volumes"`
	Workers   []Worker          `toml:"workers"`
	Jobs      []Job             `toml:"jobs"`
}

// Worker is a long-running background process from the app's build, such as a queue consumer.
type Worker struct {
	Name     string `toml:"name" json:"name"`
	Command  string `toml:"command" json:"command"`
	Replicas int    `toml:"replicas" json:"replicas,omitempty"`
}

// Job runs a command from the app's build on a cron schedule, in the server's time zone.
type Job struct {
	Name     string `toml:"name" json:"name"`
	Schedule string `toml:"schedule" json:"schedule"`
	Command  string `toml:"command" json:"command"`
}

// Volume is a named directory that outlives the app's containers.
type Volume struct {
	Name string `toml:"name" json:"name"`
	Path string `toml:"path" json:"path"`
	// Owner is the app holding a volume this app shares with others in its stack; empty means this app holds it.
	Owner string `toml:"owner" json:"owner,omitempty"`
}

// DockerName is the volume's name on the server, for an app that mounts it.
func (v Volume) DockerName(app string) string {
	return VolumeName(v.Holder(app), v.Name)
}

// Holder is the app whose volume this is.
func (v Volume) Holder(app string) string {
	if v.Owner != "" {
		return v.Owner
	}
	return app
}

const MaxVolumes = 5

// VolumeName is the docker volume backing an app's volume; app names never contain "_", so no two apps can share one.
func VolumeName(app, name string) string {
	return "skiff-" + app + "_" + name
}

// ValidateVolumes checks names and mount paths, so every deploy path accepts the same set.
func ValidateVolumes(vols []Volume) error {
	if len(vols) > MaxVolumes {
		return fmt.Errorf("at most %d volumes per app", MaxVolumes)
	}
	names, paths := map[string]bool{}, map[string]bool{}
	for _, v := range vols {
		if !validName(v.Name) {
			return fmt.Errorf("volume name %q: use 1-32 lowercase letters, digits or dashes", v.Name)
		}
		if !validMountPath(v.Path) {
			return fmt.Errorf("volume %q: path must be an absolute directory like /data", v.Name)
		}
		if names[v.Name] {
			return fmt.Errorf("volume %q is listed twice", v.Name)
		}
		if paths[v.Path] {
			return fmt.Errorf("two volumes mount at %s", v.Path)
		}
		names[v.Name], paths[v.Path] = true, true
	}
	return nil
}

func validName(n string) bool {
	if n == "" || len(n) > 32 || n[0] == '-' {
		return false
	}
	for _, r := range n {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

func validMountPath(p string) bool {
	if !strings.HasPrefix(p, "/") || p == "/" || len(p) > 200 || strings.ContainsAny(p, ":,\n\x00") {
		return false
	}
	if filepath.Clean(p) != p {
		return false
	}
	for _, sys := range []string{"/proc", "/sys", "/dev"} {
		if p == sys || strings.HasPrefix(p, sys+"/") {
			return false
		}
	}
	return true
}

type Deploy struct {
	Release       string `toml:"release"`
	Network       string `toml:"network"`
	HealthPath    string `toml:"health_path"`    // must answer 2xx/3xx before traffic moves; empty = any reply at /
	HealthTimeout int    `toml:"health_timeout"` // seconds; 0 = the default
	// DrainTimeout is how many seconds a replaced version may keep finishing open requests; unset means the default, 0 stops it straight away.
	DrainTimeout *int `toml:"drain_timeout"`
	// HealthMode is "tcp" to accept any open port, or "none" for a service with no port to check; empty means HTTP.
	HealthMode string   `toml:"health_mode"`
	Aliases    []string `toml:"aliases"` // extra names on the network
	Command    []string `toml:"command"` // replaces the image's default command
	RunOptions string   `toml:"run_options"`
}

// ValidateHealth checks a health-check path and timeout (seconds, 0 for the default).
func ValidateHealth(path string, timeout int) error {
	if path != "" {
		if !strings.HasPrefix(path, "/") || len(path) > 200 {
			return fmt.Errorf("health check path must start with / (e.g. /healthz)")
		}
		for _, r := range path {
			if r <= ' ' || r == 0x7f {
				return fmt.Errorf("health check path can't contain spaces or control characters")
			}
		}
	}
	if timeout != 0 && (timeout < 5 || timeout > 600) {
		return fmt.Errorf("health check timeout must be 5-600 seconds")
	}
	return nil
}

const (
	DefaultDrainTimeout = 300
	MaxDrainTimeout     = 3600
)

// DrainSeconds is how long a replaced version gets to finish open requests. A service with no port takes none through the router, so it has nothing to finish.
func (d Deploy) DrainSeconds() int {
	switch {
	case d.HealthMode == "none":
		return 0
	case d.DrainTimeout != nil:
		return *d.DrainTimeout
	}
	return DefaultDrainTimeout
}

// DefaultPort is the port an app is assumed to listen on when nothing says otherwise.
const DefaultPort = 8080

// ValidateImage checks an image reference like nginx:1.27 or ghcr.io/you/api@sha256:… without contacting a registry.
func ValidateImage(ref string) error {
	if ref == "" || len(ref) > 255 || ref[0] == '-' {
		return fmt.Errorf("image must be a reference like nginx:1.27 or ghcr.io/you/app:latest")
	}
	for _, r := range ref {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-/:@", r)) {
			return fmt.Errorf("image %q contains %q — use a reference like nginx:1.27", ref, r)
		}
	}
	return nil
}

// RegistryHost is the registry an image reference is pulled from; Docker Hub when the first part isn't a hostname.
func RegistryHost(ref string) string {
	first, _, found := strings.Cut(ref, "/")
	if found && (strings.ContainsAny(first, ".:") || first == "localhost") {
		return first
	}
	return "docker.io"
}

// ValidateResources checks a replica's memory and CPU limits; empty means no limit of its own.
func ValidateResources(memory, cpu string) error {
	if memory != "" {
		n, unit := memory[:len(memory)-1], memory[len(memory)-1]
		v, err := strconv.ParseFloat(n, 64)
		if err != nil || (unit != 'm' && unit != 'g') || v <= 0 || (unit == 'm' && v < 16) {
			return fmt.Errorf("memory limit must look like 512m or 2g, and be at least 16m")
		}
	}
	if cpu != "" {
		v, err := strconv.ParseFloat(cpu, 64)
		if err != nil || v <= 0 || v > 1024 {
			return fmt.Errorf("CPU limit must be a number of cores, like 0.5 or 2")
		}
	}
	return nil
}

type Resources struct {
	Memory string `toml:"memory"`
	CPU    string `toml:"cpu"`
}

type Server struct {
	Host string `toml:"host"`
}

type Build struct {
	Dockerfile string `toml:"dockerfile"`
	Port       int    `toml:"port"`

	// Image, when set, deploys a prebuilt image instead of building; with no port set, the one the image exposes is used.
	Image string `toml:"image"`

	// Optional recipe overrides: setting start or static skips auto-detection and builds from these instead of a Dockerfile.
	Base    string `toml:"base"`
	Install string `toml:"install"`
	Build   string `toml:"build"`
	Start   string `toml:"start"`
	Static  string `toml:"static"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var c Config
	if err := toml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	c.applyDefaults()
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) applyDefaults() {
	if c.Build.Dockerfile == "" {
		c.Build.Dockerfile = "Dockerfile"
	}
	if c.Build.Port == 0 && c.Build.Image == "" {
		c.Build.Port = DefaultPort
	}
	if c.Replicas < 1 {
		c.Replicas = 1
	}
}

func (c *Config) validate() error {
	if c.Name == "" {
		return fmt.Errorf("skiff.toml is missing required field: name")
	}
	if c.Build.Image != "" {
		if err := ValidateImage(c.Build.Image); err != nil {
			return err
		}
	}
	if err := ValidateHost(c.Server.Host); err != nil {
		return err
	}
	if d := c.Deploy.DrainTimeout; d != nil && (*d < 0 || *d > MaxDrainTimeout) {
		return fmt.Errorf("drain_timeout must be 0-%d seconds", MaxDrainTimeout)
	}
	if err := ValidateHealth(c.Deploy.HealthPath, c.Deploy.HealthTimeout); err != nil {
		return err
	}
	if err := ValidateVolumes(c.Volumes); err != nil {
		return err
	}
	if err := ValidateWorkers(c.Workers); err != nil {
		return err
	}
	return ValidateJobs(c.Jobs)
}

// MaxWorkerReplicas is how many copies of one worker may run.
const MaxWorkerReplicas = 10

// ValidateWorkers checks the workers listed in skiff.toml.
func ValidateWorkers(workers []Worker) error {
	names := map[string]bool{}
	for _, w := range workers {
		if !validName(w.Name) {
			return fmt.Errorf("worker name %q: use 1-32 lowercase letters, digits or dashes", w.Name)
		}
		if names[w.Name] {
			return fmt.Errorf("worker %q is listed twice", w.Name)
		}
		names[w.Name] = true
		if strings.TrimSpace(w.Command) == "" {
			return fmt.Errorf("worker %q needs a command", w.Name)
		}
		if w.Replicas < 0 || w.Replicas > MaxWorkerReplicas {
			return fmt.Errorf("worker %q: replicas must be 1-%d", w.Name, MaxWorkerReplicas)
		}
	}
	return nil
}

// ValidateJobs checks the scheduled jobs listed in skiff.toml.
func ValidateJobs(jobs []Job) error {
	names := map[string]bool{}
	for _, j := range jobs {
		if !validName(j.Name) {
			return fmt.Errorf("job name %q: use 1-32 lowercase letters, digits or dashes", j.Name)
		}
		if names[j.Name] {
			return fmt.Errorf("job %q is listed twice", j.Name)
		}
		names[j.Name] = true
		if strings.TrimSpace(j.Command) == "" {
			return fmt.Errorf("job %q needs a command", j.Name)
		}
		if _, err := cron.ParseStandard(j.Schedule); err != nil {
			return fmt.Errorf("job %q: schedule %q isn't cron syntax like \"0 3 * * *\"", j.Name, j.Schedule)
		}
	}
	return nil
}

var hostRe = regexp.MustCompile(`^[A-Za-z0-9._@:-]+$`)

// ValidateHost accepts [user@]host[:port] and nothing ssh or docker could read as an option or a different URL.
func ValidateHost(h string) error {
	if h == "" || h == "local" {
		return nil
	}
	if !hostRe.MatchString(h) || strings.HasPrefix(h, "-") {
		return fmt.Errorf("server host %q isn't a valid [user@]host", h)
	}
	return nil
}

func (c *Config) IsLocal() bool {
	return c.Server.Host == "" || c.Server.Host == "local"
}

func (c *Config) TargetLabel() string {
	if c.IsLocal() {
		return "local docker"
	}
	return c.Server.Host
}

func (c *Config) RemoteHost() string {
	if c.IsLocal() {
		return ""
	}
	return c.Server.Host
}

func (c *Config) Environment(dir string) map[string]string {
	env := loadDotenv(filepath.Join(dir, ".env"))
	for k, v := range c.Env {
		env[k] = v
	}
	return env
}

func loadDotenv(path string) map[string]string {
	env := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return env
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if k != "" {
			env[k] = v
		}
	}
	return env
}
