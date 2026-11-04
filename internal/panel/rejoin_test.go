package panel

import (
	"slices"
	"testing"

	"github.com/nuelScript/skiff/internal/auth"
)

func TestSharedNetworkStaysWithTheFoundingTeam(t *testing.T) {
	openTestDB(t)
	store := auth.NewStore(sqlDB)
	_, founding, _ := store.CreateUser("founder@acme.dev", "Founder", "password123")
	_, guest, _ := store.CreateUser("guest@corp.dev", "Guest", "password123")

	if got := rejoinNetworks(dbRow{Team: founding.ID}, []string{"skiff-t-a"}); !slices.Contains(got, dbNetwork) {
		t.Errorf("the founding team's database left the shared network: %v", got)
	}
	got := rejoinNetworks(dbRow{Team: guest.ID}, []string{"skiff-t-b", dbNetwork})
	if slices.Contains(got, dbNetwork) || !slices.Contains(got, "skiff-t-b") {
		t.Errorf("another team's database should keep only its own networks: %v", got)
	}
}
