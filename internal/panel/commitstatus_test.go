package panel

import (
	"strings"
	"testing"
)

func TestCommitStatus(t *testing.T) {
	p := &Panel{domain: "example.com"}
	prod := Source{App: "shop", Repo: "acme/shop"}
	preview := Source{App: "shop-feat-x", Repo: "acme/shop", Parent: "shop"}

	cases := []struct {
		src                 Source
		status, state, link string
	}{
		{prod, "building", "pending", "https://dash.example.com/projects/shop"},
		{prod, "live", "success", "https://shop.example.com"},
		{prod, "failed", "failure", "https://dash.example.com/projects/shop"},
		{prod, "canceled", "error", "https://dash.example.com/projects/shop"},
		{preview, "live", "success", "https://shop-feat-x.example.com"},
		{preview, "failed", "failure", "https://dash.example.com/projects/shop"},
	}
	for _, c := range cases {
		state, desc, link := p.commitStatus(c.src, c.status)
		if state != c.state || link != c.link || desc == "" {
			t.Errorf("%s %s: got %s %q %s", c.src.App, c.status, state, desc, link)
		}
	}
	if _, desc, _ := p.commitStatus(prod, "live"); !strings.Contains(desc, "https://shop.example.com") {
		t.Errorf("live description should carry the URL: %q", desc)
	}
	if statusContext(prod) != "Skiff / shop" || statusContext(preview) != "Skiff / shop preview" {
		t.Errorf("contexts: %q %q", statusContext(prod), statusContext(preview))
	}
}
