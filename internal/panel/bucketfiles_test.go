package panel

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nuelScript/skiff/internal/auth"
)

func TestValidObjectKey(t *testing.T) {
	for _, ok := range []string{"a.txt", "docs/a b.txt", "é/ü.png", "a..b/c.txt", "with+plus"} {
		if !validObjectKey(ok) {
			t.Errorf("%q was refused", ok)
		}
	}
	for _, bad := range []string{"", "/a", "a/", "a//b", "../a", "a/../b", "a/./b", "..", "a\nb", "a\x00b", "\xff"} {
		if validObjectKey(bad) {
			t.Errorf("%q was accepted", bad)
		}
	}
	for _, ok := range []string{"", "docs/", "docs/old/"} {
		if !validObjectPrefix(ok) {
			t.Errorf("prefix %q was refused", ok)
		}
	}
	for _, bad := range []string{"docs", "/", "../", "a//"} {
		if validObjectPrefix(bad) {
			t.Errorf("prefix %q was accepted", bad)
		}
	}
}

// Another team's member gets nowhere, even knowing the bucket's id.
func TestBucketFilesStayInTheTeam(t *testing.T) {
	openTestDB(t)
	store := auth.NewStore(sqlDB)
	p := &Panel{auth: store}
	_, owning, err := store.CreateUser("a@acme.dev", "A", "password123")
	if err != nil {
		t.Fatal(err)
	}
	other, otherTeam, err := store.CreateUser("b@acme.dev", "B", "password123")
	if err != nil {
		t.Fatal(err)
	}
	_ = putBucket(bucketRow{ID: "bkt1", Team: owning.ID, Name: "media", Container: "skiff-s3-bkt1"})
	_ = putSession("other", other.ID, otherTeam.ID)
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(method, "/api/storage/object?id=bkt1&key=a.txt", nil)
		req.AddCookie(&http.Cookie{Name: "skiff_session", Value: "other"})
		w := httptest.NewRecorder()
		p.handleBucketObject(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s: %d", method, w.Code)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/api/storage/objects?id=bkt1", nil)
	req.AddCookie(&http.Cookie{Name: "skiff_session", Value: "other"})
	w := httptest.NewRecorder()
	p.handleBucketObjects(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("list: %d", w.Code)
	}
}
