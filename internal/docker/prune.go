package docker

import (
	"strings"
	"time"
)

type AppImage struct {
	App     string
	Ref     string
	Created time.Time
}

// AppImages lists every skiff-<app> image tag on the engine.
func (e *Engine) AppImages() ([]AppImage, error) {
	out, err := e.command("images", "--filter", "reference=skiff-*",
		"--format", "{{.Repository}}\t{{.Tag}}\t{{.CreatedAt}}").Output()
	if err != nil {
		return nil, err
	}
	var imgs []AppImage
	for _, ln := range splitLines(out) {
		f := strings.SplitN(ln, "\t", 3)
		if len(f) != 3 || f[1] == "<none>" || !strings.HasPrefix(f[0], "skiff-") {
			continue
		}
		imgs = append(imgs, AppImage{
			App:     strings.TrimPrefix(f[0], "skiff-"),
			Ref:     f[0] + ":" + f[1],
			Created: parseCreatedAt(f[2]),
		})
	}
	return imgs, nil
}

// ContainerImages returns the image of every container, running or not.
func (e *Engine) ContainerImages() (map[string]bool, error) {
	out, err := e.command("ps", "-a", "--format", "{{.Image}}").Output()
	if err != nil {
		return nil, err
	}
	used := map[string]bool{}
	for _, img := range splitLines(out) {
		used[img] = true
	}
	return used, nil
}

func (e *Engine) PruneDanglingImages() error {
	out, err := e.command("image", "prune", "-f").CombinedOutput()
	if err != nil {
		return cmdErr(out, err)
	}
	return nil
}

func (e *Engine) PruneBuildCache(olderThan time.Duration) error {
	out, err := e.command("builder", "prune", "-f", "--filter", "until="+olderThan.String()).CombinedOutput()
	if err != nil {
		return cmdErr(out, err)
	}
	return nil
}

func parseCreatedAt(s string) time.Time {
	t, err := time.Parse("2006-01-02 15:04:05 -0700 MST", strings.TrimSpace(s))
	if err != nil {
		return time.Now()
	}
	return t
}
