package panel

import (
	"testing"
	"time"
)

func TestProjectDeploysFiltersAndCoversPreviews(t *testing.T) {
	openTestDB(t)
	_ = putSource(Source{App: "shop", Team: "t", Repo: "a/shop", Branch: "main", Port: "3000"})
	_ = putSource(Source{App: "shop-feat", Team: "t", Repo: "a/shop", Branch: "feat/x", Port: "3000", Parent: "shop"})
	_ = putSource(Source{App: "other", Team: "t", Repo: "a/other", Branch: "main", Port: "3000"})
	now := time.Now().Unix()
	add := func(id, app, branch, status, message string, at int64) {
		addDeploy(Deploy{ID: id, App: app, Commit: "c" + id, Message: message, Trigger: "push", Status: status, Started: at, Branch: branch, Actor: "dev"})
	}
	add("1", "shop", "main", "live", "fix 100% of login", now-30)
	add("2", "shop-feat", "feat/x", "building", "add cart", now-20)
	add("3", "shop", "main", "failed", "bump deps", now-10)
	add("4", "other", "main", "live", "unrelated", now)

	ids := func(ds []Deploy) string {
		out := ""
		for _, d := range ds {
			out += d.ID
		}
		return out
	}
	for _, tc := range []struct {
		name string
		f    deployFilter
		want string
	}{
		{"everything", deployFilter{}, "321"},
		{"production", deployFilter{env: "production"}, "31"},
		{"preview", deployFilter{env: "preview"}, "2"},
		{"status", deployFilter{status: "failed"}, "3"},
		{"branch", deployFilter{branch: "feat/x"}, "2"},
		{"search message", deployFilter{search: "cart"}, "2"},
		{"search commit", deployFilter{search: "c3"}, "3"},
		{"a percent sign is literal", deployFilter{search: "100%"}, "1"},
		{"no wildcards from search", deployFilter{search: "%"}, "1"},
	} {
		if got := ids(projectDeploys("shop", tc.f, 0, "", 30)); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
	all := projectDeploys("shop", deployFilter{}, 0, "", 30)
	if all[1].Parent != "shop" || all[0].Parent != "" {
		t.Errorf("only preview deploys name a parent: %+v", all)
	}
	if page := projectDeploys("shop", deployFilter{}, all[0].Started, all[0].ID, 30); ids(page) != "21" {
		t.Errorf("paging: %q", ids(page))
	}
	if current := currentDeploy("shop"); current != "1" {
		t.Errorf("a failed deploy isn't current, got %q", current)
	}
	if got := deployBranches("shop"); len(got) != 2 || got[0] != "feat/x" || got[1] != "main" {
		t.Errorf("branches: %v", got)
	}
}

func TestDeployRecordsWhenItEnded(t *testing.T) {
	openTestDB(t)
	started := time.Now().Unix() - 100
	addDeploy(Deploy{ID: "a", App: "shop", Status: "building", Started: started})
	if d := appDeploysPage("shop", 0, "", 1)[0]; d.Finished != 0 {
		t.Fatalf("still building, but finished=%d", d.Finished)
	}
	setDeployStatus("shop", "a", "live")
	d := appDeploysPage("shop", 0, "", 1)[0]
	if d.Finished == 0 {
		t.Fatal("a finished deploy has no end time")
	}
	first := d.Finished
	if _, err := sqlDB.Exec(`UPDATE deploys SET finished=? WHERE id='a'`, first-5); err != nil {
		t.Fatal(err)
	}
	setDeployStatus("shop", "a", "failed")
	if got := appDeploysPage("shop", 0, "", 1)[0].Finished; got != first-5 {
		t.Errorf("a later status moved the end time: %d", got)
	}
}
