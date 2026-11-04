package docker

import (
	"encoding/json"
	"strings"
)

const (
	labelVolumeApp  = "skiff.volume.app"
	labelVolumeName = "skiff.volume"
)

// EnsureAppVolume creates an app's named volume, labelled with its owner so it can be listed and removed with the app; an existing volume is left as is.
func (e *Engine) EnsureAppVolume(volume, app, name string) error {
	if e.VolumeExists(volume) {
		return nil
	}
	out, err := e.command("volume", "create",
		"--label", labelVolumeApp+"="+app,
		"--label", labelVolumeName+"="+name,
		volume).CombinedOutput()
	if err != nil {
		return cmdErr(out, err)
	}
	return nil
}

// AppVolumes names every volume created for an app.
func (e *Engine) AppVolumes(app string) []string {
	out, err := e.command("volume", "ls", "-q", "--filter", "label="+labelVolumeApp+"="+app).Output()
	if err != nil {
		return nil
	}
	return splitLines(out)
}

// VolumeSizes reports how much each volume holds; docker walks every volume to answer, so callers should cache it.
func (e *Engine) VolumeSizes() map[string]int64 {
	out, err := e.command("system", "df", "-v", "--format", "{{json .Volumes}}").Output()
	if err != nil {
		return nil
	}
	return parseVolumeSizes(out)
}

func parseVolumeSizes(out []byte) map[string]int64 {
	var vols []struct{ Name, Size string }
	if json.Unmarshal([]byte(strings.TrimSpace(string(out))), &vols) != nil {
		return nil
	}
	sizes := make(map[string]int64, len(vols))
	for _, v := range vols {
		sizes[v.Name] = parseSize(v.Size)
	}
	return sizes
}

// RunningUsing names the running containers that mount a volume.
func (e *Engine) RunningUsing(volume string) []string {
	out, err := e.command("ps", "--format", "{{.Names}}", "--filter", "volume="+volume, "--filter", "status=running").Output()
	if err != nil {
		return nil
	}
	return splitLines(out)
}

// PauseUsing freezes the containers that mount a volume, so a copy of it is consistent, and returns the ones it froze.
func (e *Engine) PauseUsing(volume string) []string {
	var paused []string
	for _, c := range e.RunningUsing(volume) {
		if e.command("pause", c).Run() == nil {
			paused = append(paused, c)
		}
	}
	return paused
}

// Unpause resumes frozen containers.
func (e *Engine) Unpause(containers []string) {
	for _, c := range containers {
		_ = e.command("unpause", c).Run()
	}
}

// UnpauseLeftovers resumes containers a crash left frozen mid-backup.
func (e *Engine) UnpauseLeftovers() {
	out, err := e.command("ps", "-q", "--filter", "status=paused", "--filter", "label=skiff=1").Output()
	if err == nil {
		e.Unpause(splitLines(out))
	}
}

func mountArgs(mounts []Mount) []string {
	var args []string
	for _, m := range mounts {
		args = append(args, "-v", m.Name+":"+m.Destination)
	}
	return args
}
