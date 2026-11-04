package cmd

import "testing"

func TestDockerfileInside(t *testing.T) {
	for _, ok := range []string{"Dockerfile", "docker/Dockerfile.prod", "./Dockerfile"} {
		if !dockerfileInside(ok) {
			t.Errorf("%q should be allowed", ok)
		}
	}
	for _, bad := range []string{"../Dockerfile", "a/../../x", "/etc/passwd", ".."} {
		if dockerfileInside(bad) {
			t.Errorf("%q should be refused", bad)
		}
	}
}
