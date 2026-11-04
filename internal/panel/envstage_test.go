package panel

import (
	"testing"
	"time"

	"github.com/nuelScript/skiff/internal/registry"
)

func TestEnvStageReservation(t *testing.T) {
	openTestDB(t)
	e := envStage
	now := time.Unix(1_700_000_000, 0)

	if !e.reserve("api", "teamA", now) {
		t.Fatal("teamA could not reserve a free name")
	}
	if e.reserve("api", "teamB", now) {
		t.Fatal("teamB reserved a name teamA already holds")
	}
	if !e.heldByOther("api", "teamB", now) {
		t.Fatal("name should read as held by another team for teamB")
	}
	if e.heldByOther("api", "teamA", now) {
		t.Fatal("the holding team must not see its own name as foreign")
	}

	e.reserve("web", "teamA", now)
	e.release("web")
	if !e.reserve("web", "teamB", now) {
		t.Fatal("teamB could not reserve after release")
	}
}

func TestEnvStageSurvivesRestartAndNeverHandsOverVars(t *testing.T) {
	openTestDB(t)
	now := time.Unix(1_700_000_000, 0)
	if !envStage.reserve("api", "teamA", now) {
		t.Fatal("reserve")
	}
	if err := setEnv("api", []EnvVar{{Key: "SECRET", Value: "a-only"}}); err != nil {
		t.Fatal(err)
	}

	// Nothing is held in memory, so a restarted panel sees the same owner.
	if envStage.reserve("api", "teamB", now.Add(time.Minute)) {
		t.Fatal("teamB took a name teamA holds")
	}

	// Once the reservation has lapsed the name can change hands, but not the vars.
	later := now.Add(envStageTTL + time.Minute)
	if !envStage.reserve("api", "teamB", later) {
		t.Fatal("teamB should be able to take a lapsed name")
	}
	if got := getEnv("api"); len(got) != 0 {
		t.Fatalf("teamB inherited teamA's staged vars: %+v", got)
	}

	// Vars with no recorded owner are not handed to whoever asks first.
	if err := setEnv("orphan", []EnvVar{{Key: "K", Value: "v"}}); err != nil {
		t.Fatal(err)
	}
	if !envStage.reserve("orphan", "teamB", now) {
		t.Fatal("reserve orphan")
	}
	if got := getEnv("orphan"); len(got) != 0 {
		t.Fatalf("unowned vars were readable: %+v", got)
	}
}

func TestAdoptedCLIAppDoesNotInheritAnotherTeamsStagedVars(t *testing.T) {
	openTestDB(t)
	if !envStage.reserve("billing", "teamA", time.Now()) {
		t.Fatal("reserve")
	}
	if err := setEnv("billing", []EnvVar{{Key: "STRIPE_KEY", Value: "sk"}}); err != nil {
		t.Fatal(err)
	}
	adopted := adoptCLIApps(map[string]registry.App{"billing": {Name: "billing", Port: 3000}}, "teamB")
	if len(adopted) != 1 {
		t.Fatalf("adopted = %v", adopted)
	}
	if got := getEnv("billing"); len(got) != 0 {
		t.Fatalf("teamB inherited teamA's staged vars: %+v", got)
	}
	if _, _, held := envStage.owner("billing"); held {
		t.Fatal("the reservation should be gone once the app exists")
	}
}
