package panel

import (
	"fmt"
	"log"
	"slices"
	"strings"
)

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
	for _, d := range allDatabases() {
		for _, app := range dbAttachments(d.ID) {
			if err := p.linkResource(d.Container, d.network(), app); err != nil {
				log.Printf("networks: %s for %s: %v", d.Name, app, err)
			}
		}
	}
	for _, b := range allBuckets() {
		for _, app := range bucketAttachments(b.ID) {
			if err := p.linkResource(b.Container, teamNetwork(b.Team), app); err != nil {
				log.Printf("networks: %s for %s: %v", b.Name, app, err)
			}
		}
	}
}

// linkResource connects a database or bucket to the network of an app attached to it, when that's not the one it lives on. Connecting the resource rather than the app keeps a stack apart from the rest of its team, and holds for every way an app's containers start.
func (p *Panel) linkResource(container, home, app string) error {
	src, ok := getSource(app)
	if !ok {
		return nil
	}
	net := appNetwork(src)
	if net == home {
		return nil
	}
	if err := p.eng.EnsureNetwork(net); err != nil {
		return fmt.Errorf("creating %s: %w", net, err)
	}
	if err := p.eng.ConnectNetwork(net, container); err != nil && !strings.Contains(err.Error(), "already exists") {
		return fmt.Errorf("joining %s: %w", net, err)
	}
	return nil
}

// unlinkResource undoes linkResource once no attached app is left on that network. Call it after removing the attachment.
func (p *Panel) unlinkResource(container, home, net string, attached []string) {
	if net == home {
		return
	}
	for _, app := range attached {
		if src, ok := getSource(app); ok && appNetwork(src) == net {
			return
		}
	}
	_ = p.eng.DisconnectNetwork(net, container)
}

// releaseAttachments runs before an app is deleted, so its stack's network has nothing left on it and can be removed.
func (p *Panel) releaseAttachments(app string) {
	src, ok := getSource(app)
	if !ok {
		return
	}
	net := appNetwork(src)
	for _, a := range appDatabaseAttachments(app) {
		d, ok := getDB(a.ID)
		if !ok {
			continue
		}
		detachDB(a.ID, app)
		p.unlinkResource(d.Container, d.network(), net, dbAttachments(a.ID))
	}
	for _, b := range allBuckets() {
		if !slices.Contains(bucketAttachments(b.ID), app) {
			continue
		}
		_, _ = sqlDB.Exec(`DELETE FROM bucket_attachments WHERE bucket_id=? AND app=?`, b.ID, app)
		p.unlinkResource(b.Container, teamNetwork(b.Team), net, bucketAttachments(b.ID))
	}
}
