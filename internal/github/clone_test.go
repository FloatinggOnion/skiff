package github

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestIsCloneURLOf(t *testing.T) {
	ok := []string{
		"https://github.com/acme/api.git",
		"https://github.com/acme/api",
		"https://github.com/Acme/API.git",
		"https://github.com/acme/api/",
	}
	for _, u := range ok {
		if !IsCloneURLOf(u, "acme/api") {
			t.Errorf("%q should match", u)
		}
	}
	bad := []string{
		"https://evil.example/acme/api.git",
		"https://github.com.evil.example/acme/api.git",
		"https://evil.example/github.com/acme/api.git",
		"https://github.com@evil.example/acme/api.git",
		"https://x:y@github.com/acme/api.git",
		"http://github.com/acme/api.git",
		"https://github.com/acme/other.git",
		"https://github.com/acme/api.git?x=1",
		"https://github.com:444/acme/api.git",
		"",
	}
	for _, u := range bad {
		if IsCloneURLOf(u, "acme/api") {
			t.Errorf("%q should not match", u)
		}
	}
	if IsCloneURLOf("https://github.com/acme/api.git", "acme/api/../x") || IsCloneURLOf("https://github.com/acme/api.git", "") {
		t.Error("a malformed repo should never match")
	}
}

func TestValidRepo(t *testing.T) {
	for _, s := range []string{"acme/api", "a.b/c-d_e"} {
		if !ValidRepo(s) {
			t.Errorf("%q should be valid", s)
		}
	}
	for _, s := range []string{"", "acme", "acme/api/x", "../x", "acme/ api", "a/b\n"} {
		if ValidRepo(s) {
			t.Errorf("%q should be invalid", s)
		}
	}
}

func TestCloneAuthEnvKeepsTokenOutOfTheURL(t *testing.T) {
	env := cloneAuthEnv("tok")
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "GIT_CONFIG_KEY_0=http.https://github.com/.extraheader") {
		t.Fatalf("env = %v", env)
	}
	want := base64.StdEncoding.EncodeToString([]byte("x-access-token:tok"))
	if !strings.Contains(joined, "Authorization: basic "+want) {
		t.Fatalf("header missing: %v", env)
	}
}
