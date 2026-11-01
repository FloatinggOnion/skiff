package github

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func fakeGitHub(t *testing.T, handler http.HandlerFunc) *Config {
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	prev := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = prev })
	tokMu.Lock()
	tokVal, tokExp = "test-token", time.Now().Add(time.Hour)
	tokMu.Unlock()
	return &Config{ID: 1, InstallationID: 2}
}

func TestPreviewCommentFlow(t *testing.T) {
	comments := map[int64]string{}
	next := int64(100)
	gh := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token test-token" {
			t.Errorf("missing token on %s", r.URL)
		}
		var body struct{ Body string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch {
		case r.Method == "GET" && r.URL.Path == "/repos/acme/shop/pulls":
			if r.URL.Query().Get("head") != "acme:feature" {
				t.Errorf("head = %q", r.URL.Query().Get("head"))
			}
			_, _ = w.Write([]byte(`[{"number":7}]`))
		case r.Method == "POST" && r.URL.Path == "/repos/acme/shop/issues/7/comments":
			next++
			comments[next] = body.Body
			_, _ = w.Write([]byte(`{"id":` + itoa(next) + `}`))
		case r.Method == "PATCH":
			var id int64
			for k := range comments {
				if r.URL.Path == "/repos/acme/shop/issues/comments/"+itoa(k) {
					id = k
				}
			}
			if id == 0 {
				http.NotFound(w, r)
				return
			}
			comments[id] = body.Body
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
	})

	pr, err := gh.OpenPullRequest("acme/shop", "feature")
	if err != nil || pr != 7 {
		t.Fatalf("pr %d err %v", pr, err)
	}
	id, err := gh.UpsertComment("acme/shop", 7, 0, "building")
	if err != nil || id != 101 {
		t.Fatalf("create: id %d err %v", id, err)
	}
	if id2, err := gh.UpsertComment("acme/shop", 7, id, "ready"); err != nil || id2 != id || comments[id] != "ready" {
		t.Fatalf("edit: id %d err %v comments %v", id2, err, comments)
	}
	delete(comments, id) // someone deleted the comment
	if id3, err := gh.UpsertComment("acme/shop", 7, id, "ready again"); err != nil || id3 == id || comments[id3] != "ready again" {
		t.Fatalf("repost: id %d err %v", id3, err)
	}
}

func TestMissingPermissionIsForbidden(t *testing.T) {
	gh := fakeGitHub(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"Resource not accessible by integration"}`, http.StatusForbidden)
	})
	if _, err := gh.UpsertComment("acme/shop", 7, 0, "x"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v", err)
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestSetStatus(t *testing.T) {
	var got map[string]string
	gh := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/repos/acme/shop/statuses/abc123" {
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
	})
	long := string(make([]byte, 200))
	if err := gh.SetStatus("acme/shop", "abc123", "success", "Skiff / shop", long, "https://shop.example.com"); err != nil {
		t.Fatal(err)
	}
	if got["state"] != "success" || got["context"] != "Skiff / shop" || got["target_url"] != "https://shop.example.com" || len(got["description"]) != 140 {
		t.Fatalf("got %v", got)
	}
}
