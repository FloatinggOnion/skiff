package docker

import (
	"encoding/json"
	"regexp"
	"strings"
)

// VolumeTool is the small image Skiff runs to work on a volume's files.
const VolumeTool = "alpine:3.20"

const (
	labelVolumeApp  = "skiff.volume.app"
	labelVolumeName = "skiff.volume"
)

// EnsureAppVolume creates an app's named volume, labelled with its owner so it can be listed and removed with the app; an existing volume is left as is.
func (e *Engine) EnsureAppVolume(volume, app, name string) error {
	_, err := e.ensureAppVolume(volume, app, name)
	return err
}

// EnsureAppVolumeFor is EnsureAppVolume for a container that runs image as user (empty means the image's own user). A new volume is handed to that user, as Docker only does when the image already has the folder, so an image that doesn't run as root can write to it.
func (e *Engine) EnsureAppVolumeFor(volume, app, name, image, user string) error {
	created, err := e.ensureAppVolume(volume, app, name)
	if err != nil || !created {
		return err
	}
	if owner := e.runsAs(image, user); owner != "" {
		_ = e.command("run", "--rm", "-v", volume+":/v", VolumeTool, "chown", owner, "/v").Run()
	}
	return nil
}

func (e *Engine) ensureAppVolume(volume, app, name string) (created bool, err error) {
	if e.VolumeExists(volume) {
		return false, nil
	}
	out, err := e.command("volume", "create",
		"--label", labelVolumeApp+"="+app,
		"--label", labelVolumeName+"="+name,
		volume).CombinedOutput()
	if err != nil {
		return false, cmdErr(out, err)
	}
	return true, nil
}

var (
	numericUser = regexp.MustCompile(`^\d+(:\d+)?$`)
	idOutput    = regexp.MustCompile(`uid=(\d+).*?gid=(\d+)`)
)

// runsAs is the uid[:gid] a container of image runs as, or "" for root or when it can't be told.
func (e *Engine) runsAs(image, user string) string {
	if user == "" {
		out, err := e.command("image", "inspect", "--format", "{{.Config.User}}", image).Output()
		if err != nil {
			return ""
		}
		user = strings.TrimSpace(string(out))
	}
	switch {
	case user == "" || user == "root" || user == "0" || strings.HasPrefix(user, "0:") || strings.HasPrefix(user, "root:"):
		return ""
	case numericUser.MatchString(user):
		return user
	}
	// A named user only means something inside the image.
	out, err := e.command("run", "--rm", "--user", user, "--entrypoint", "id", image).Output()
	if err != nil {
		return ""
	}
	if m := idOutput.FindSubmatch(out); m != nil && string(m[1]) != "0" {
		return string(m[1]) + ":" + string(m[2])
	}
	return ""
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
