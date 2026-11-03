package panel

import (
	"strings"
	"testing"
	"time"
)

func TestStackPreviewName(t *testing.T) {
	if got := stackPreviewName("shop", "feat/Login"); got != "shop-feat-login" {
		t.Fatalf("got %q", got)
	}
	long := strings.Repeat("a", 60)
	one, two := stackPreviewName("shop", long+"x"), stackPreviewName("shop", long+"y")
	if len(one) > 40 || one == two {
		t.Fatalf("long branches should stay short and distinct: %q, %q", one, two)
	}
}

func TestStackPreviewCommentBody(t *testing.T) {
	body := stackPreviewCommentBody("shop-feat-login", "abc1234", []stackPreviewRow{
		{service: "web", status: "live", url: "https://shop-feat-login-web.example.com"},
		{service: "api", status: "failed"},
	}, time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC))
	for _, want := range []string{"| web | ✅ Ready | https://shop-feat-login-web.example.com |", "| api | ❌ Failed | private |", "`abc1234`"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in:\n%s", want, body)
		}
	}
}
