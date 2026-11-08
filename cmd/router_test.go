package cmd

import "testing"

func TestDefaultCertsDir(t *testing.T) {
	cases := []struct {
		goos, home, want string
	}{
		{"linux", "/root", "/var/lib/skiff/certs"}, // unchanged for every existing install
		{"linux", "/home/me", "/var/lib/skiff/certs"},
		{"darwin", "/Users/me", "/Users/me/.skiff/certs"},
		{"darwin", "/Users/Jo Doe", "/Users/Jo Doe/.skiff/certs"},
	}
	for _, c := range cases {
		if got := defaultCertsDir(c.goos, c.home); got != c.want {
			t.Errorf("defaultCertsDir(%q, %q) = %q, want %q", c.goos, c.home, got, c.want)
		}
	}
}
