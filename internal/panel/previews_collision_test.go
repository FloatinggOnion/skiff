package panel

import "testing"

func TestPreviewCannotTakeOverAnotherTeamsApp(t *testing.T) {
	p, founder, other := twoTeams(t)
	victim := Source{App: "shop-api", Team: other, Port: "3000", Image: "x:1"}
	if err := putSource(victim); err != nil {
		t.Fatal(err)
	}
	parent := Source{App: "shop", Team: founder, Repo: "acme/shop", Branch: "main", Port: "3000", CloneURL: "https://203.0.113.7/acme/shop"}
	if err := putSource(parent); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := p.createPreview(parent, "api", "", ""); ok {
		t.Fatal("a preview named after another team's app must be refused")
	}
	got, _ := getSource("shop-api")
	if got.Team != other || got.Image != "x:1" {
		t.Fatalf("the other team's app was changed: %+v", got)
	}

	// Same team, but not this project's preview: still not ours to overwrite.
	mine := Source{App: "shop-web", Team: founder, Port: "3000", Image: "y:1"}
	if err := putSource(mine); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := p.createPreview(parent, "web", "", ""); ok {
		t.Fatal("a preview must not overwrite an app that isn't one of its own")
	}
}

func TestPutSourceKeepsTheOwningTeam(t *testing.T) {
	_, founder, other := twoTeams(t)
	if err := putSource(Source{App: "api", Team: founder, Port: "3000"}); err != nil {
		t.Fatal(err)
	}
	if err := putSource(Source{App: "api", Team: other, Port: "4000"}); err == nil {
		t.Fatal("writing an app under another team must fail")
	}
	got, _ := getSource("api")
	if got.Team != founder || got.Port != "3000" {
		t.Fatalf("row changed: %+v", got)
	}
	if err := putSource(Source{App: "api", Team: founder, Port: "5000"}); err != nil {
		t.Fatalf("the owner can still update: %v", err)
	}
	if got, _ := getSource("api"); got.Port != "5000" {
		t.Fatalf("update lost: %+v", got)
	}
}
