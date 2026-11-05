package config

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// RunOptions are settings for how a container starts, from a Compose file. None of them grants anything beyond what a container already has.
type RunOptions struct {
	Entrypoint  []string `json:"entrypoint,omitempty"`
	User        string   `json:"user,omitempty"`
	WorkingDir  string   `json:"workingDir,omitempty"`
	Init        bool     `json:"init,omitempty"`
	ReadOnly    bool     `json:"readOnly,omitempty"`
	ShmSize     string   `json:"shmSize,omitempty"`
	Tmpfs       []string `json:"tmpfs,omitempty"`
	Ulimits     []string `json:"ulimits,omitempty"`
	ExtraHosts  []string `json:"extraHosts,omitempty"`
	StopSeconds int      `json:"stopSeconds,omitempty"`
	StopSignal  string   `json:"stopSignal,omitempty"`
}

// IsZero reports whether no option is set.
func (o RunOptions) IsZero() bool { return reflect.ValueOf(o).IsZero() }

// Equal reports whether two sets of options start a container the same way.
func (o RunOptions) Equal(other RunOptions) bool { return reflect.DeepEqual(o, other) }

var (
	shmSizePattern    = regexp.MustCompile(`^[0-9]+[bkmgBKMG]?$`)
	ulimitPattern     = regexp.MustCompile(`^(nofile|nproc|memlock)=[0-9]+(:[0-9]+)?$`)
	tmpfsPattern      = regexp.MustCompile(`^/[^\s:,]*(:[A-Za-z0-9=,]+)?$`)
	extraHostPattern  = regexp.MustCompile(`^[A-Za-z0-9.-]+:[A-Fa-f0-9.:]+$|^[A-Za-z0-9.-]+:host-gateway$`)
	stopSignalPattern = regexp.MustCompile(`^[A-Za-z0-9+]+$`)
)

// Validate refuses a value docker would misread, such as one that looks like another flag.
func (o RunOptions) Validate() error {
	for label, v := range map[string]string{"user": o.User, "working_dir": o.WorkingDir} {
		if strings.HasPrefix(v, "-") || strings.ContainsAny(v, "\n\r") {
			return fmt.Errorf("%s has an invalid value", label)
		}
	}
	if o.WorkingDir != "" && !strings.HasPrefix(o.WorkingDir, "/") {
		return fmt.Errorf("working_dir must be an absolute path")
	}
	if o.ShmSize != "" && !shmSizePattern.MatchString(o.ShmSize) {
		return fmt.Errorf("shm_size must look like 256m")
	}
	for _, t := range o.Tmpfs {
		if !tmpfsPattern.MatchString(t) {
			return fmt.Errorf("tmpfs %q must be an absolute path", t)
		}
	}
	for _, u := range o.Ulimits {
		if !ulimitPattern.MatchString(u) {
			return fmt.Errorf("ulimits %q isn't one Skiff sets: use nofile, nproc or memlock", u)
		}
	}
	for _, h := range o.ExtraHosts {
		if !extraHostPattern.MatchString(h) {
			return fmt.Errorf("extra_hosts %q must look like name:address", h)
		}
	}
	if o.StopSeconds < 0 || o.StopSeconds > 3600 {
		return fmt.Errorf("stop_grace_period must be between 0 and an hour")
	}
	if o.StopSignal != "" && !stopSignalPattern.MatchString(o.StopSignal) {
		return fmt.Errorf("stop_signal has an invalid value")
	}
	return nil
}

// DockerArgs are the flags for docker run, which go before the image.
func (o RunOptions) DockerArgs() []string {
	var args []string
	if len(o.Entrypoint) > 0 {
		args = append(args, "--entrypoint", o.Entrypoint[0])
	}
	add := func(flag, v string) {
		if v != "" {
			args = append(args, flag, v)
		}
	}
	add("--user", o.User)
	add("--workdir", o.WorkingDir)
	add("--shm-size", o.ShmSize)
	add("--stop-signal", o.StopSignal)
	if o.StopSeconds > 0 {
		args = append(args, "--stop-timeout", strconv.Itoa(o.StopSeconds))
	}
	if o.Init {
		args = append(args, "--init")
	}
	if o.ReadOnly {
		args = append(args, "--read-only")
	}
	for _, t := range o.Tmpfs {
		args = append(args, "--tmpfs", t)
	}
	for _, u := range o.Ulimits {
		args = append(args, "--ulimit", u)
	}
	for _, h := range o.ExtraHosts {
		args = append(args, "--add-host", h)
	}
	return args
}

// Command is what goes after the image: docker takes one word as the entrypoint, so the rest come first.
func (o RunOptions) Command(command []string) []string {
	if len(o.Entrypoint) < 2 {
		return command
	}
	return append(append([]string{}, o.Entrypoint[1:]...), command...)
}

// EncodeRunOptions is the stored form, empty when nothing is set.
func EncodeRunOptions(o RunOptions) string {
	if o.IsZero() {
		return ""
	}
	b, err := json.Marshal(o)
	if err != nil {
		return ""
	}
	return string(b)
}

// DecodeRunOptions reads the stored form.
func DecodeRunOptions(s string) RunOptions {
	var o RunOptions
	if s != "" {
		_ = json.Unmarshal([]byte(s), &o)
	}
	return o
}
