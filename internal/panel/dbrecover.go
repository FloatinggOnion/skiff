package panel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/nuelScript/skiff/internal/docker"
)

// recoverDatabases brings back any managed database whose container is gone — after a restore onto a new server, or a manual removal. When its volume went too, the newest offsite backup is loaded into the fresh one; an existing volume still has the data, so it's never overwritten.
func (p *Panel) recoverDatabases() {
	for _, d := range allDatabases() {
		if p.eng.State(d.Container) != "missing" {
			continue
		}
		freshVolume := !p.eng.VolumeExists(d.volume())
		if err := p.recreateDatabase(d); err != nil {
			log.Printf("skiff: recreating database %s: %v", d.Name, err)
			continue
		}
		log.Printf("skiff: recreated missing database %s", d.Name)
		if !freshVolume {
			continue
		}
		p.resetPITR(d) // its history belongs to the data that's gone
		if key, err := p.restoreLatestOffsite(d); err != nil {
			log.Printf("skiff: database %s came back empty: %v", d.Name, err)
		} else if key != "" {
			log.Printf("skiff: loaded %s into %s", key, d.Name)
			recordAudit(d.Team, AuditEntry{Actor: "skiff", Action: "backup.restore", Target: d.Name, Detail: "recovered from offsite " + key})
		}
	}
}

func (p *Panel) recreateDatabase(d dbRow) error {
	e := dbEngines[d.Engine]
	env, cmd := e.container(d.Username, d.Password, d.DBName)
	if d.Image != "" {
		cmd = nil
		_ = json.Unmarshal([]byte(d.Cmd), &cmd)
	}
	net := d.network()
	if err := p.eng.EnsureNetwork(net); err != nil {
		return err
	}
	pitrCmd, pitrBinds, err := p.pitrRunArgs(d, cmd)
	if err != nil {
		return err
	}
	_, err = p.eng.RunDatabase(docker.DBRunSpec{
		Name: d.Container, Image: d.image(), Network: net,
		Volume: d.volume(), MountAt: e.mountAt, Env: env, Cmd: pitrCmd, Binds: pitrBinds, Aliases: d.aliases(),
		Labels: map[string]string{"skiff.kind": "database", "skiff.db": d.ID, "skiff.team": d.Team},
	})
	if err != nil {
		return err
	}
	if d.Stack == "" {
		_ = p.eng.ConnectNetwork(dbNetwork, d.Container)
	}
	for _, app := range dbAttachments(d.ID) {
		_ = p.linkResource(d.Container, d.network(), app)
	}
	_, _ = sqlDB.Exec(`UPDATE databases SET public=0, public_port=0 WHERE id=?`, d.ID)
	return nil
}

// restoreLatestOffsite downloads the database's newest offsite copy and loads it, retrying while the fresh database finishes starting.
func (p *Panel) restoreLatestOffsite(d dbRow) (string, error) {
	if !dbEngines[d.Engine].canRestore() {
		return "", nil
	}
	var key string
	if sqlDB.QueryRow(`SELECT key FROM offsite_objects WHERE db_id=? ORDER BY created DESC LIMIT 1`, d.ID).Scan(&key) != nil {
		return "", nil
	}
	t := getOffsite(d.Team)
	if !t.configured() {
		return "", fmt.Errorf("no offsite bucket configured for its team")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	rc, err := t.client().Get(ctx, key)
	if err != nil {
		return "", err
	}
	tmp := filepath.Join(backupsDir(), d.ID, "recover-"+filepath.Base(key))
	if err := os.MkdirAll(filepath.Dir(tmp), 0o700); err != nil {
		rc.Close()
		return "", err
	}
	f, err := os.Create(tmp)
	if err != nil {
		rc.Close()
		return "", err
	}
	_, err = io.Copy(f, rc)
	rc.Close()
	f.Close()
	defer os.Remove(tmp)
	if err != nil {
		return "", err
	}

	var last error
	for range 30 {
		in, err := os.Open(tmp)
		if err != nil {
			return "", err
		}
		last = p.loadBackup(ctx, d, in)
		in.Close()
		if last == nil {
			return key, nil
		}
		time.Sleep(3 * time.Second)
	}
	return "", last
}
