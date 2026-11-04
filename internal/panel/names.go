package panel

import "os"

// reservedNames are subdomains the router or the box itself answers on, so no team's project may take them.
var reservedNames = map[string]bool{"www": true, "dash": true, "status": true, "mx": true}

// nameReserved reports whether a new project of this name is off limits to the team. The team that set the box up may use them, as its own site usually runs as www. Projects that already exist are never checked.
func nameReserved(name, team string) bool {
	if team != "" && team == foundingTeam() {
		return false
	}
	return reservedNames[name] || (os.Getenv("SKIFF_SITE_APP") != "" && name == os.Getenv("SKIFF_SITE_APP"))
}

const reservedNameMsg = "that name is reserved: pick another"
