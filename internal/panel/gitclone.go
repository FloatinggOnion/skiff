package panel

import (
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/nuelScript/skiff/internal/github"
)

// cloneHostAllowed refuses hosts that resolve to private, loopback or link-local addresses, so a clone can't be pointed at services on the box or its network.
func cloneHostAllowed(host string) bool {
	if host == "" {
		return false
	}
	if drainsAllowPrivate() {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return !isBlockedDialIP(ip)
	}
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return false
	}
	for _, ip := range ips {
		if isBlockedDialIP(ip) {
			return false
		}
	}
	return true
}

func cloneURLHostAllowed(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && cloneHostAllowed(u.Hostname())
}

// withinDir reports whether p is root or inside it.
func withinDir(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// checkSymlinks fails when a link in a fresh checkout leads outside it, since the build and the config readers follow links on the panel's own filesystem.
func checkSymlinks(root string) error {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.Type()&fs.ModeSymlink == 0 {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		bad := fmt.Errorf("the repository has a symlink that points outside it: %s", rel)
		target, err := os.Readlink(path)
		if err != nil || filepath.IsAbs(target) {
			return bad
		}
		if !withinDir(root, filepath.Join(filepath.Dir(path), target)) {
			return bad
		}
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			if !withinDir(realRoot, resolved) {
				return bad
			}
		} else if !os.IsNotExist(err) {
			return bad
		}
		return nil
	})
}

// githubUsable reports whether a team may use the GitHub App: its installation spans the whole box, so only the team that set it up does.
func githubUsable(team string) bool {
	return team != "" && team == foundingTeam()
}

// appCloneEnv is the App's token as git config for the founding team's clone of the repository it's installed on, and nothing otherwise.
func appCloneEnv(team, cloneURL, repo string) []string {
	if repo == "" || !githubUsable(team) {
		return nil
	}
	gh := github.Load()
	if !gh.Installed() {
		return nil
	}
	env, _ := gh.CloneEnv(cloneURL, repo)
	return env
}

// gitCloneEnv keeps git from prompting or reaching command-running transports, and adds any extra settings.
func gitCloneEnv(extra []string) []string {
	return append(append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ALLOW_PROTOCOL=https:http"), extra...)
}

// insideCheckout resolves dir and reports whether it still lies in the checkout.
func insideCheckout(root, dir string) bool {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	real, err := filepath.EvalSymlinks(dir)
	return err == nil && withinDir(realRoot, real)
}
