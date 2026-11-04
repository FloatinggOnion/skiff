package panel

import "testing"

func TestTunnelsPerTeamAreCapped(t *testing.T) {
	var releases []func()
	for range tunnelsPerTeam {
		release, ok := holdTunnel("a")
		if !ok {
			t.Fatal("refused a tunnel below the cap")
		}
		releases = append(releases, release)
	}
	if _, ok := holdTunnel("a"); ok {
		t.Fatal("went over the cap")
	}
	other, ok := holdTunnel("b")
	if !ok {
		t.Fatal("one team's tunnels blocked another team")
	}
	other()
	releases[0]()
	again, ok := holdTunnel("a")
	if !ok {
		t.Fatal("closing a tunnel didn't free its place")
	}
	again()
	for _, release := range releases[1:] {
		release()
	}
	if len(openTunnels) != 0 {
		t.Fatalf("counts left behind: %v", openTunnels)
	}
}
