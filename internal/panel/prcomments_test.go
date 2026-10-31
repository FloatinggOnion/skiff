package panel

import (
	"strings"
	"testing"
	"time"
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
