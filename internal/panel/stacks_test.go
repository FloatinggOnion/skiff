package panel

import (
	"errors"
	"strings"
	"testing"

	"github.com/nuelScript/skiff/internal/compose"
	"github.com/nuelScript/skiff/internal/config"
	"github.com/nuelScript/skiff/internal/seal"
)

func TestGithubRepo(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/acme/shop":      "acme/shop",
		"https://github.com/acme/shop.git":  "acme/shop",
		"https://github.com/acme/shop/":     "acme/shop",
		"https://gitlab.com/acme/shop":      "",
		"https://github.com/acme":           "",
		"https://github.com/acme/shop/tree": "",
	} {
		if got := githubRepo(in); got != want {
			t.Errorf("%s = %q, want %q", in, got, want)
		}
	}
}

func TestStackAppsFollowPushes(t *testing.T) {
	openTestDB(t)
	s := &stackSource{cloneURL: "https://github.com/acme/shop", branch: "main", dir: "/deploy",
		plan: &compose.Plan{Stack: "shop"}}
	built := stackSourceFor(s, "t1", compose.Item{Service: "api", Name: "shop-api", Build: &compose.Build{Context: "../api", Dockerfile: "Dockerfile.prod"}, Port: 3000, Public: true})
	pulled := stackSourceFor(s, "t1", compose.Item{Service: "search", Name: "shop-search", Image: "meili:1", Port: 7700})
	for _, src := range []Source{built, pulled} {
		if err := putSource(src); err != nil {
			t.Fatal(err)
		}
	}
	if built.RootDir != "api" || built.Dockerfile != "Dockerfile.prod" || built.HealthMode != "" {
		t.Errorf("built: %+v", built)
	}
	if pulled.HealthMode != "tcp" || !pulled.Private || pulled.Image != "meili:1" {
		t.Errorf("pulled: %+v", pulled)
	}
	got := sourcesForRepo("acme/shop", "main")
	if len(got) != 1 || got[0].App != "shop-api" {
		t.Fatalf("a push should redeploy the built app only: %+v", got)
	}
	back, ok := getSource("shop-api")
	if !ok || back.Stack != "shop" || back.Service != "api" {
		t.Fatalf("stored: %+v", back)
	}
}

func updateFixture(t *testing.T) (*stackSource, currentStack) {
	t.Helper()
	item := compose.Item{Service: "api", Name: "shop-api", Kind: compose.KindApp, Image: "app:1", Port: 3000, Public: true, Replicas: 1,
		Env: map[string]string{"A": "1", "SECRET": "old-secret"}, Volumes: []compose.PlanVolume{{Name: "data", Path: "/data"}}}
	db := compose.Item{Service: "db", Name: "shop-db", Kind: compose.KindDatabase, Image: "postgres:16", Engine: "postgres", DBUser: "app", DBPassword: "pw", DB: "shop"}
	s := &stackSource{plan: &compose.Plan{Stack: "shop", Items: []compose.Item{db, item}}}
	src := stackSourceFor(s, "t", item)
	return s, currentStack{
		Apps:    map[string]Source{"api": src},
		Env:     map[string]map[string]string{"api": {"A": "1", "SECRET": "old-secret"}},
		Volumes: map[string][]config.Volume{"api": {{Name: "data", Path: "/data"}}},
		Workers: map[string]workerRow{},
		DBs:     map[string]dbRow{"db": {Alias: "db", Image: "postgres:16", Username: "app", Password: "pw", DBName: "shop"}},
	}
}

func TestDiffStack(t *testing.T) {
	s, cur := updateFixture(t)
	if changes, problems := diffStack(s, "t", cur); len(changes) != 0 || len(problems) != 0 {
		t.Fatalf("no difference expected: %+v %v", changes, problems)
	}

	s, cur = updateFixture(t)
	api := &s.plan.Items[1]
	api.Image, api.Env = "app:2", map[string]string{"A": "2", "SECRET": "new-secret", "B": "3"}
	changes, _ := diffStack(s, "t", cur)
	if len(changes) != 1 || changes[0].Action != "update" || changes[0].DeletesData {
		t.Fatalf("%+v", changes)
	}
	details := strings.Join(changes[0].Details, "\n")
	for _, want := range []string{"image: app:1 → app:2", "adds env: B", "changes env: A, SECRET"} {
		if !strings.Contains(details, want) {
			t.Errorf("details %q missing %q", details, want)
		}
	}
	if strings.Contains(details, "new-secret") || strings.Contains(details, "old-secret") {
		t.Errorf("details leak a secret: %q", details)
	}

	s, cur = updateFixture(t)
	s.plan.Items[1].Volumes = nil
	if changes, _ := diffStack(s, "t", cur); len(changes) != 1 || !changes[0].DeletesData {
		t.Errorf("dropping a volume must be marked: %+v", changes)
	}

	s, cur = updateFixture(t)
	s.plan.Items = s.plan.Items[1:]
	changes, _ = diffStack(s, "t", cur)
	if len(changes) != 1 || changes[0].Action != "remove" || changes[0].Kind != "database" || !changes[0].DeletesData {
		t.Errorf("dropping a database must be marked: %+v", changes)
	}

	s, cur = updateFixture(t)
	s.plan.Items = s.plan.Items[:1]
	changes, _ = diffStack(s, "t", cur)
	if len(changes) != 1 || changes[0].Action != "remove" || !changes[0].DeletesData {
		t.Errorf("dropping an app with a volume must be marked: %+v", changes)
	}

	for name, edit := range map[string]func(*compose.Item){
		"image":    func(i *compose.Item) { i.Image = "postgres:17" },
		"username": func(i *compose.Item) { i.DBUser = "other" },
		"password": func(i *compose.Item) { i.DBPassword = "other" },
		"database": func(i *compose.Item) { i.DB = "other" },
	} {
		s, cur = updateFixture(t)
		edit(&s.plan.Items[0])
		if _, problems := diffStack(s, "t", cur); len(problems) != 1 || !strings.Contains(problems[0], "isn't applied to existing data") {
			t.Errorf("%s: %v", name, problems)
		}
	}

	// A generated password isn't a change when the file doesn't name one.
	s, cur = updateFixture(t)
	s.plan.Items[0].DBPassword = ""
	if _, problems := diffStack(s, "t", cur); len(problems) != 0 {
		t.Errorf("no password in the file: %v", problems)
	}
}

func TestHoldReason(t *testing.T) {
	if got := holdReason(stackChangesView{Changes: []StackChange{{Service: "api", Action: "update"}, {Service: "w", Action: "remove"}}}, nil); got != "" {
		t.Errorf("safe changes should apply: %q", got)
	}
	if got := holdReason(stackChangesView{Changes: []StackChange{{Service: "db", Action: "remove", DeletesData: true}}, DeletesData: true}, nil); !strings.Contains(got, "delete data in db") {
		t.Errorf("deleting data: %q", got)
	}
	if got := holdReason(stackChangesView{Problems: []string{"X needs a value"}}, nil); got != "X needs a value" {
		t.Errorf("problems: %q", got)
	}
	if got := holdReason(stackChangesView{}, errors.New("bad yaml")); !strings.Contains(got, "bad yaml") {
		t.Errorf("unreadable file: %q", got)
	}
}

func TestStackVarsAreSealed(t *testing.T) {
	openTestDB(t)
	sealed, err := sealStackVars(map[string]string{"DB_PASSWORD": "hunter2"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, "hunter2") || !seal.IsSealed(sealed) {
		t.Fatalf("stored as %q", sealed)
	}
	if _, err := sqlDB.Exec(`INSERT INTO stacks(name,team,created,vars) VALUES('s','t',1,?)`, sealed); err != nil {
		t.Fatal(err)
	}
	r, ok := getStackRecord("s")
	if !ok || r.Vars["DB_PASSWORD"] != "hunter2" {
		t.Fatalf("%+v", r)
	}
}
