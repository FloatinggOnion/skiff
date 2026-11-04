package cmd

import (
	"github.com/nuelScript/skiff/internal/docker"
	"github.com/nuelScript/skiff/internal/registry"
)

// reach gives the host and port to use from this machine for an app's published port. Apps listen only on their own server's loopback, so a remote one is reached through an ssh tunnel.
func reach(eng *docker.Engine, target string, port int) (host string, hostPort int, stop func(), err error) {
	if !eng.IsRemote() {
		return "127.0.0.1", port, func() {}, nil
	}
	local, stop, err := docker.Forward(target, port)
	if err != nil {
		return "", 0, nil, err
	}
	return "127.0.0.1", local, stop, nil
}

// probeApp reports whether a registered app answers, through a tunnel when it runs on another machine.
func probeApp(a registry.App) bool {
	host, port, stop, err := reach(docker.For(a.Host), a.Host, a.HostPort)
	if err != nil {
		return false
	}
	defer stop()
	return probe(host, port)
}
