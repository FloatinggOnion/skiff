package docker

import (
	"sort"
	"strconv"
	"strings"
)

func (e *Engine) Stop(container string) error {
	defer invalidateStates()
	out, err := e.command("stop", container).CombinedOutput()
	if err != nil {
		return cmdErr(out, err)
	}
	return nil
}

// StopWithin asks a container to exit and gives it this many seconds before killing it.
func (e *Engine) StopWithin(container string, seconds int) error {
	defer invalidateStates()
	out, err := e.command("stop", "-t", strconv.Itoa(seconds), container).CombinedOutput()
	if err != nil {
		return cmdErr(out, err)
	}
	return nil
}

func (e *Engine) Remove(name string) error {
	defer invalidateStates()
	out, err := e.command("rm", "-f", name).CombinedOutput()
	if err != nil {
		return cmdErr(out, err)
	}
	return nil
}

func (e *Engine) Tag(src, dst string) error {
	out, err := e.command("tag", src, dst).CombinedOutput()
	if err != nil {
		return cmdErr(out, err)
	}
	return nil
}

// AppImageTags lists an app's retained image tags (excluding :latest and <none>), relying on docker's default created-descending order — newest first — for pruning.
func (e *Engine) AppImageTags(app string) []string {
	out, err := e.command("images", "skiff-"+app, "--format", "{{.Tag}}").Output()
	if err != nil {
		return nil
	}
	var tags []string
	for _, t := range splitLines(out) {
		if t == "latest" || t == "<none>" {
			continue
		}
		tags = append(tags, t)
	}
	sortDeployTags(tags)
	return tags
}

// sortDeployTags puts the newest deploy first. Deploy ids start with their time. Docker's own order can't be used, because it's arbitrary among tags of one image, which is every redeploy of unchanged code.
func sortDeployTags(tags []string) {
	started := func(tag string) int64 {
		head, _, _ := strings.Cut(tag, "-")
		n, _ := strconv.ParseInt(head, 10, 64)
		return n
	}
	sort.SliceStable(tags, func(i, j int) bool { return started(tags[i]) > started(tags[j]) })
}

func (e *Engine) ImageExists(tag string) bool {
	return e.command("image", "inspect", tag).Run() == nil
}

func (e *Engine) RemoveImage(tag string) error {
	return e.command("rmi", tag).Run()
}

func (e *Engine) Start(container string) error {
	defer invalidateStates()
	out, err := e.command("start", container).CombinedOutput()
	if err != nil {
		return cmdErr(out, err)
	}
	return nil
}

func (e *Engine) VolumeExists(name string) bool {
	return e.command("volume", "inspect", name).Run() == nil
}
