package panel

import "log"

// Each team's apps/databases join a private docker network (skiff-t-<team>) so a compromised app can't reach another team's containers by name.

func teamNetwork(team string) string {
	if team == "" {
		return dbNetwork
	}
	return "skiff-t-" + sanitizeName(team)
}

// reconcileNetworks attaches every existing database to its team's private network so an app redeployed onto the team net still finds it by name.
func (p *Panel) reconcileNetworks() {
	rows, err := sqlDB.Query(`SELECT team, container FROM databases WHERE stack=''`)
	if err != nil {
		return
	}
	type dbNet struct{ team, container string }
	var dbs []dbNet
	for rows.Next() {
		var d dbNet
		if rows.Scan(&d.team, &d.container) == nil {
			dbs = append(dbs, d)
		}
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		return
	}

	for _, d := range dbs {
		if d.team == "" || d.container == "" {
			continue
		}
		net := teamNetwork(d.team)
		if p.eng.EnsureNetwork(net) != nil {
			continue
		}
		_ = p.eng.ConnectNetwork(net, d.container) // already-connected is a harmless error
	}
	if len(dbs) > 0 {
		log.Printf("networks: reconciled %d databases onto team networks", len(dbs))
	}
}
