// Package config loads and validates a skiff.toml describing a single app.
package config

import (
	"fmt"
	"os"
	"path/filepath"
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
	Release string `toml:"release"`
	Network string `toml:"network"`
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
	if c.Build.Port == 0 {
		c.Build.Port = 8080
	}
	if c.Replicas < 1 {
		c.Replicas = 1
	}
}

func (c *Config) validate() error {
	if c.Name == "" {
		return fmt.Errorf("skiff.toml is missing required field: name")
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
