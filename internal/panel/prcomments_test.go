package panel

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/nuelScript/skiff/internal/github"
)

func TestPreviewCommentBody(t *testing.T) {
	at := time.Date(2026, 10, 31, 14, 5, 0, 0, time.UTC)
	b := previewCommentBody("shop-feature", "https://shop-feature.example.com", "abc1234", "live", at)
	for _, want := range []string{"✅ Ready", "https://shop-feature.example.com", "`abc1234`", "`shop-feature`", "Oct 31, 14:05 UTC"} {
		if !strings.Contains(b, want) {
			t.Errorf("comment missing %q:\n%s", want, b)
		}
	}
	if !strings.Contains(previewCommentBody("a", "u", "", "failed", at), "❌ Failed") {
		t.Error("failed state not shown")
	}
}

func TestCommentOnPRSkipsNonPreviews(t *testing.T) {
	openTestDB(t)
	// A production app (no parent) must never reach GitHub, even with nothing configured.
	(&Panel{domain: "example.com"}).commentOnPR(Source{App: "shop", Repo: "acme/shop", Branch: "main"}, "abc", "live")
}

func TestPreviewsToCreate(t *testing.T) {
	openTestDB(t)
	for _, s := range []Source{
		{App: "site", Repo: "acme/mono", Branch: "main", RootDir: "web/site", PreviewAuto: true},
		{App: "api", Repo: "acme/mono", Branch: "main", RootDir: "", PreviewAuto: true},
		{App: "docs", Repo: "acme/mono", Branch: "main", RootDir: "docs"},
		{App: "site-feat-y", Repo: "acme/mono", Branch: "feat/y", RootDir: "web/site", Parent: "site"},
	} {
		if err := putSource(s); err != nil {
			t.Fatal(err)
		}
	}
	names := func(push github.Push) string {
		var out []string
		for _, s := range previewsToCreate(push) {
			out = append(out, s.App)
		}
		sort.Strings(out)
		return strings.Join(out, ",")
	}
	cases := []struct {
		push github.Push
		want string
	}{
		{github.Push{Repo: "acme/mono", Branch: "feat/x", Paths: []string{"internal/x.go"}}, "api"},
		{github.Push{Repo: "acme/mono", Branch: "feat/x", Paths: []string{"web/site/app/page.tsx"}}, "api,site"},
		{github.Push{Repo: "acme/mono", Branch: "feat/x", Paths: []string{"web/sitemap.ts"}}, "api"},
		{github.Push{Repo: "acme/mono", Branch: "feat/x"}, "api,site"},
		{github.Push{Repo: "acme/mono", Branch: "feat/y", Paths: []string{"web/site/a.tsx"}}, "api"},
		{github.Push{Repo: "acme/mono", Branch: "main", Paths: []string{"web/site/a.tsx"}}, ""},
		{github.Push{Repo: "acme/other", Branch: "feat/x"}, ""},
	}
	for _, c := range cases {
		if got := names(c.push); got != c.want {
			t.Errorf("%s %v: got %q, want %q", c.push.Branch, c.push.Paths, got, c.want)
		}
	}
}

func TestBranchPreviews(t *testing.T) {
	openTestDB(t)
	for _, s := range []Source{
		{App: "site", Repo: "acme/mono", Branch: "feat/x"},
		{App: "site-feat-x", Repo: "acme/mono", Branch: "feat/x", Parent: "site"},
		{App: "api-feat-x", Repo: "acme/mono", Branch: "feat/x", Parent: "api"},
		{App: "site-feat-y", Repo: "acme/mono", Branch: "feat/y", Parent: "site"},
		{App: "other-feat-x", Repo: "acme/other", Branch: "feat/x", Parent: "other"},
	} {
		if err := putSource(s); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	for _, s := range branchPreviews("acme/mono", "feat/x") {
		got = append(got, s.App)
	}
	sort.Strings(got)
	if strings.Join(got, ",") != "api-feat-x,site-feat-x" {
		t.Fatalf("got %v; a production app on that branch must never be torn down", got)
	}
}
