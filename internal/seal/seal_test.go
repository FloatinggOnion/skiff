package seal

import (
	"bytes"
	"strings"
	"testing"
)

func load(t *testing.T, b byte) {
	t.Helper()
	if err := Init(bytes.Repeat([]byte{b}, 32)); err != nil {
		t.Fatal(err)
	}
}

func TestRoundTrip(t *testing.T) {
	load(t, 1)
	s, err := Seal("hunter2")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(s, "hunter2") || !IsSealed(s) {
		t.Fatalf("not sealed: %q", s)
	}
	if got, err := Open(s); err != nil || got != "hunter2" {
		t.Fatalf("open: %q %v", got, err)
	}
	if again, _ := Seal("hunter2"); again == s {
		t.Fatal("two seals of one value must differ")
	}
}

func TestEmptyStaysEmpty(t *testing.T) {
	load(t, 1)
	if s, err := Seal(""); err != nil || s != "" {
		t.Fatalf("got %q %v", s, err)
	}
}

func TestOldPlainValuesPassThrough(t *testing.T) {
	load(t, 1)
	if got, err := Open("postgres://u:p@h/db"); err != nil || got != "postgres://u:p@h/db" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestWrongKeyAndDamageFail(t *testing.T) {
	load(t, 1)
	s, _ := Seal("hunter2")
	flipped := s[:len(s)-2] + "AA"
	if flipped == s {
		flipped = s[:len(s)-2] + "BB"
	}
	if _, err := Open(flipped); err == nil {
		t.Fatal("damaged value opened")
	}
	if _, err := Open(prefix + "!!"); err == nil {
		t.Fatal("bad encoding opened")
	}
	load(t, 2)
	if _, err := Open(s); err == nil {
		t.Fatal("wrong key opened")
	}
}

func TestOpenAllNeverLeaksCiphertext(t *testing.T) {
	load(t, 1)
	s, _ := Seal("hunter2")
	load(t, 2)
	field, plain := s, "plain"
	if err := OpenAll(&field, &plain); err == nil {
		t.Fatal("expected an error")
	}
	if field != "" || plain != "plain" {
		t.Fatalf("field=%q plain=%q", field, plain)
	}
}
