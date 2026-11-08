package panel

import (
	"strings"
	"testing"
)

func TestPushTouchesSelf(t *testing.T) {
	p := &Panel{}
	for paths, want := range map[string]bool{
		"web/site/app/page.tsx":                   false,
		"web/docs/content/docs/compose.mdx":       false,
		"web/site/a.tsx,web/docs/b.mdx":           false,
		"web/docs/content/docs/cli.mdx,cmd/db.go": true,
		"internal/panel/panel.go":                 true,
		"":                                        true,
	} {
		var list []string
		if paths != "" {
			list = strings.Split(paths, ",")
		}
		if got := p.pushTouchesSelf(list); got != want {
			t.Errorf("%q: got %v, want %v", paths, got, want)
		}
	}
}
