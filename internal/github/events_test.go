package github

import "testing"

func TestParsePushBranchDeleted(t *testing.T) {
	p, ok := ParsePush([]byte(`{"ref":"refs/heads/feat/x","deleted":true,"after":"0000000000000000000000000000000000000000","repository":{"full_name":"acme/shop"},"head_commit":null}`))
	if !ok || !p.Deleted || p.Repo != "acme/shop" || p.Branch != "feat/x" {
		t.Fatalf("got %+v, %v", p, ok)
	}
}

func TestParsePushIgnoresTags(t *testing.T) {
	if p, ok := ParsePush([]byte(`{"ref":"refs/tags/v1.0.0","after":"abc","repository":{"full_name":"acme/shop"},"head_commit":{"message":"v1"}}`)); ok {
		t.Fatalf("a tag push was read as a branch push: %+v", p)
	}
}

func TestParsePushPaths(t *testing.T) {
	p, ok := ParsePush([]byte(`{"ref":"refs/heads/main","after":"abc","repository":{"full_name":"acme/shop"},
		"head_commit":{"message":"feat: x\n\nbody","modified":["web/a.ts"]},
		"commits":[{"added":["go.mod"],"removed":["web/a.ts"]}]}`))
	if !ok || p.Deleted || p.Branch != "main" || p.Message != "feat: x" || len(p.Paths) != 2 {
		t.Fatalf("got %+v, %v", p, ok)
	}
}

func TestParsePullRequest(t *testing.T) {
	pr, ok := ParsePullRequest([]byte(`{"action":"opened","number":7,"repository":{"full_name":"acme/shop"},
		"pull_request":{"head":{"ref":"feat/x","repo":{"full_name":"acme/shop"}}}}`))
	if !ok || pr.Action != "opened" || pr.Repo != "acme/shop" || pr.Branch != "feat/x" || pr.Number != 7 {
		t.Fatalf("got %+v, %v", pr, ok)
	}
	for name, body := range map[string]string{
		"fork":         `{"action":"opened","number":8,"repository":{"full_name":"acme/shop"},"pull_request":{"head":{"ref":"main","repo":{"full_name":"someone/shop"}}}}`,
		"deleted fork": `{"action":"opened","number":9,"repository":{"full_name":"acme/shop"},"pull_request":{"head":{"ref":"main","repo":null}}}`,
	} {
		if _, ok := ParsePullRequest([]byte(body)); ok {
			t.Errorf("%s: pull request from outside the repo was accepted", name)
		}
	}
}
