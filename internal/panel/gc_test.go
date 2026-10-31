package panel

import (
	"testing"
	"time"

	"github.com/nuelScript/skiff/internal/docker"
)

func TestStaleAppImages(t *testing.T) {
	now := time.Now()
	old := now.Add(-48 * time.Hour)
	imgs := []docker.AppImage{
		{App: "live", Ref: "skiff-live:latest", Created: old},
		{App: "live", Ref: "skiff-live:123-abc", Created: old},
		{App: "project", Ref: "skiff-project:latest", Created: old},
		{App: "gone", Ref: "skiff-gone:latest", Created: old},
		{App: "gone", Ref: "skiff-gone:99-x", Created: old},
		{App: "stopped", Ref: "skiff-stopped:latest", Created: old},
		{App: "fresh", Ref: "skiff-fresh:latest", Created: now.Add(-time.Minute)},
	}
	got := staleAppImages(imgs,
		map[string]bool{"live": true},
		map[string]bool{"project": true},
		map[string]bool{"skiff-stopped:latest": true},
		now)
	want := []string{"skiff-gone:99-x", "skiff-gone:latest"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %v, want %v", got, want)
	}
}
