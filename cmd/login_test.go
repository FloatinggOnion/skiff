package cmd

import (
	"strings"
	"testing"
)

func TestSameMachine(t *testing.T) {
	if !sameMachine("127.0.0.1", "localhost") {
		t.Error("localhost should resolve to 127.0.0.1")
	}
	if !sameMachine("10.0.0.5", "10.0.0.5") {
		t.Error("identical IPs")
	}
	if sameMachine("10.0.0.5", "10.0.0.6") {
		t.Error("different IPs matched")
	}
	if sameMachine("no-such-host.invalid", "localhost") {
		t.Error("unresolvable host matched")
	}
}

func TestApproveURLOK(t *testing.T) {
	cases := []struct {
		approve, base string
		want          bool
	}{
		{"https://dash.example.com/cli?code=1", "https://dash.example.com", true},
		{"https://dash.example.com", "https://dash.example.com", true},
		{"http://localhost:4000/cli?code=1", "http://localhost:4000", true},
		{"https://dash.example.com/cli", "http://dash.example.com", true},
		{"https://dash.example.com.evil.test/cli", "https://dash.example.com", false},
		{"https://dash.example.com@evil.test/cli", "https://dash.example.com", false},
		{"file:///etc/passwd", "https://dash.example.com", false},
		{"https://evil.test/", "https://dash.example.com", false},
	}
	for _, c := range cases {
		if got := approveURLOK(c.approve, c.base); got != c.want {
			t.Errorf("approveURLOK(%q, %q) = %v, want %v", c.approve, c.base, got, c.want)
		}
	}
}

func TestIsLocalURL(t *testing.T) {
	for u, want := range map[string]bool{"http://localhost:4000": true, "http://127.0.0.1:1": true, "http://[::1]:1": true, "http://dash.example.com": false} {
		if isLocalURL(u) != want {
			t.Errorf("isLocalURL(%q) != %v", u, want)
		}
	}
}

func TestLoginBase(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := loginBase(nil); err == nil || !strings.Contains(err.Error(), "skiff login https://") {
		t.Errorf("with no sign-in yet, it should say how to name a dashboard: %v", err)
	}
	if got, err := loginBase([]string{" dash.example.com/ "}); err != nil || got != "https://dash.example.com" {
		t.Errorf("named dashboard: %q %v", got, err)
	}
	if err := saveCredentials(credentials{URL: "https://dash.saved.dev", Token: "t"}); err != nil {
		t.Fatal(err)
	}
	if got, err := loginBase(nil); err != nil || got != "https://dash.saved.dev" {
		t.Errorf("should reuse the saved dashboard: %q %v", got, err)
	}
	if got, err := loginBase([]string{"http://other.dev"}); err != nil || got != "http://other.dev" {
		t.Errorf("a named dashboard wins over the saved one: %q %v", got, err)
	}
}
