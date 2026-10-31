package cmd

import "testing"

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
