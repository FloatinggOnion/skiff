package panel

import (
	"log"

	"github.com/nuelScript/skiff/internal/github"
)

var statusGate = &grantGate{permission: "Commit statuses: write"}

// commitStatus maps a deploy status to a GitHub commit status and where it should link.
func (p *Panel) commitStatus(src Source, status string) (state, description, target string) {
	appURL := "https://" + src.App + "." + p.domain
	project := src.App
	if src.Parent != "" {
		project = src.Parent
	}
	logs := "https://dash." + p.domain + "/projects/" + project
	switch status {
	case "building":
		return "pending", "Building", logs
	case "live":
		return "success", "Live at " + appURL, appURL
	case "canceled":
		return "error", "Superseded by a newer deploy", logs
	default:
		return "failure", "Deploy failed — see the build log", logs
	}
}

func statusContext(src Source) string {
	if src.Parent != "" {
		return "Skiff / " + src.Parent + " preview"
	}
	return "Skiff / " + src.App
}

// statusReporter sends one deploy's statuses to GitHub in order, off the build's path; close the returned stop when the deploy ends.
func (p *Panel) statusReporter(src Source) (report func(sha, status string), stop func()) {
	ch := make(chan [2]string, 8)
	go func() {
		for s := range ch {
			p.reportStatus(src, s[0], s[1])
		}
	}()
	return func(sha, status string) {
			select {
			case ch <- [2]string{sha, status}:
			default:
			}
		}, func() {
			close(ch)
		}
}

// reportStatus shows a deploy's progress on its commit in GitHub, next to CI.
func (p *Panel) reportStatus(src Source, sha, status string) {
	if src.Repo == "" || len(sha) != 40 || statusGate.blocked() {
		return
	}
	gh := github.Load()
	if !gh.Installed() {
		return
	}
	state, desc, target := p.commitStatus(src, status)
	if err := gh.SetStatus(src.Repo, sha, state, statusContext(src), desc, target); err != nil {
		if statusGate.note(err) {
			log.Printf("skiff: commit status: %v", err)
		}
		return
	}
	statusGate.ok()
}
