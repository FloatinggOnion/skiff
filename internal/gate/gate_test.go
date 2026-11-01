package gate

import (
	"path/filepath"
	"testing"
	"time"
)

func TestPasswordAndCookie(t *testing.T) {
	h, err := Hash("hunter22")
	if err != nil {
		t.Fatal(err)
	}
	if !Check(h, "hunter22") || Check(h, "hunter2") || Check(h, "") {
		t.Fatal("password check is wrong")
	}
	now := time.Unix(1_800_000_000, 0)
	c := Cookie(h, "shop-feat", now.Add(time.Hour))
	if !ValidCookie(h, "shop-feat", c, now) {
		t.Fatal("fresh cookie rejected")
	}
	if ValidCookie(h, "shop-feat", c, now.Add(2*time.Hour)) {
		t.Error("expired cookie accepted")
	}
	if ValidCookie(h, "shop-other", c, now) {
		t.Error("cookie for one preview opened another")
	}
	h2, _ := Hash("hunter22")
	if ValidCookie(h2, "shop-feat", c, now) {
		t.Error("cookie survived a password change")
	}
	for _, bad := range []string{"", "nodot", "abc.def", "99999999999.deadbeef"} {
		if ValidCookie(h, "shop-feat", bad, now) {
			t.Errorf("accepted %q", bad)
		}
	}
	// Pushing the expiry forward must break the signature.
	forged := "99999999999" + c[len("1800003600"):]
	if ValidCookie(h, "shop-feat", forged, now) {
		t.Error("accepted a cookie with an extended expiry")
	}
}

func TestSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "protected.json")
	if err := Save(path, Apps{"a": "h"}); err != nil {
		t.Fatal(err)
	}
	if got := Load(path); got["a"] != "h" || len(got) != 1 {
		t.Fatalf("got %v", got)
	}
	if got := Load(filepath.Join(t.TempDir(), "missing.json")); len(got) != 0 {
		t.Fatalf("missing file should load empty, got %v", got)
	}
}
