package panel

import "testing"

func TestRepeatedReadsMakeOneEntry(t *testing.T) {
	key := "env\x00test-user\x00shop"
	if !firstInAWhile(key) {
		t.Fatal("the first read should be recorded")
	}
	if firstInAWhile(key) {
		t.Fatal("a repeat so soon should not")
	}
	if !firstInAWhile(key + "-other") {
		t.Fatal("another key is its own first read")
	}
}
