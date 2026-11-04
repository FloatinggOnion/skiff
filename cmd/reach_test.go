package cmd

import (
	"testing"

	"github.com/nuelScript/skiff/internal/docker"
)

func TestReachLocalUsesLoopbackDirectly(t *testing.T) {
	host, port, stop, err := reach(docker.Local(), "", 41234)
	if err != nil || host != "127.0.0.1" || port != 41234 {
		t.Fatalf("got %s:%d, %v", host, port, err)
	}
	stop()
}
