package panel

import "testing"

func TestCloneURLAllowed(t *testing.T) {
	allowed := []string{
		"https://203.0.113.7/acme/api.git",
		"http://203.0.113.7/acme/api",
		"https://x-access-token:tok@203.0.113.7/acme/api.git", // token-injected form
	}
	for _, u := range allowed {
		if !cloneURLAllowed(u) {
			t.Errorf("%q should be allowed", u)
		}
	}

	// git's command-executing / non-http transports must be rejected.
	blocked := []string{
		`ext::sh -c "id"`,
		"ext::sh -c whoami",
		"file:///etc/passwd",
		"file::/tmp/x",
		"ssh://git@github.com/acme/api",
		"git@github.com:acme/api.git",
		"",
		"   ",
		"javascript:alert(1)",
		"http://127.0.0.1/acme/api",
		"http://localhost/acme/api",
		"http://10.0.0.5:3000/acme/api",
		"http://169.254.169.254/latest",
		"http://[::1]/x",
	}
	for _, u := range blocked {
		if cloneURLAllowed(u) {
			t.Errorf("%q should be rejected", u)
		}
	}
}
