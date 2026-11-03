// Package config loads and validates a skiff.toml describing a single app.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
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
}

// Volume is a named directory that outlives the app's containers.
type Volume struct {
	Name string `toml:"name" json:"name"`
	Path string `toml:"path" json:"path"`
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
		if !validVolumeName(v.Name) {
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

func validVolumeName(n string) bool {
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
	// HealthMode is "tcp" to accept any open port, or "none" for a service with no port to check; empty means HTTP.
	HealthMode string   `toml:"health_mode"`
	Aliases    []string `toml:"aliases"` // extra names on the network
	Command    []string `toml:"command"` // replaces the image's default command
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
	if err := ValidateHealth(c.Deploy.HealthPath, c.Deploy.HealthTimeout); err != nil {
		return err
	}
	return ValidateVolumes(c.Volumes)
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
