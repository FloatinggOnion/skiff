package panel

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nuelScript/skiff/internal/cpbackup"
)

const (
	cpBackupEvery = 24 * time.Hour
	cpBackupKeep  = 14
)

// recoveryKey is the key control-plane backups are sealed with; it never leaves the box except when the owner reveals it to store somewhere safe.
func recoveryKey() ([]byte, error) {
	path := filepath.Join(skiffDir(), "recovery.key")
	if b, err := os.ReadFile(path); err == nil {
		return cpbackup.ParseKey(string(b))
	}
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(hex.EncodeToString(k)), 0o600); err != nil {
		return nil, err
	}
	defer os.Remove(tmp)
	if err := os.Link(tmp, path); err != nil {
		if b, rerr := os.ReadFile(path); rerr == nil {
			return cpbackup.ParseKey(string(b))
		}
		return nil, err
	}
	return k, nil
}

// controlPlaneBundle snapshots the live database (VACUUM INTO is consistent under WAL) and seals it with the state files.
func controlPlaneBundle() ([]byte, error) {
	key, err := recoveryKey()
	if err != nil {
		return nil, err
	}
	snap := filepath.Join(skiffDir(), fmt.Sprintf(".snapshot-%d.db", time.Now().UnixNano()))
	defer os.Remove(snap)
	if _, err := sqlDB.Exec(`VACUUM INTO ?`, snap); err != nil {
		return nil, fmt.Errorf("snapshotting the database: %w", err)
	}
	var buf bytes.Buffer
	if err := cpbackup.Pack(&buf, skiffDir(), snap); err != nil {
		return nil, err
	}
	return cpbackup.Seal(key, buf.Bytes())
}

type cpBackup struct {
	Key     string `json:"key"`
	Size    int64  `json:"size"`
	Created int64  `json:"created"`
}

func (p *Panel) runControlPlaneBackup() (cpBackup, error) {
	team := foundingTeam()
	t := getOffsite(team)
	if !t.configured() {
		return cpBackup{}, fmt.Errorf("set up an offsite bucket in Settings first")
	}
	sealed, err := controlPlaneBundle()
	if err != nil {
		return cpBackup{}, err
	}
	now := time.Now()
	key := "control-plane/skiff-" + now.UTC().Format("20060102T150405Z") + ".skiffbak"
	if pre := strings.Trim(t.Prefix, "/"); pre != "" {
		key = pre + "/" + key
	}
	c := t.client()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := c.Put(ctx, key, bytes.NewReader(sealed), int64(len(sealed))); err != nil {
		return cpBackup{}, err
	}
	b := cpBackup{Key: key, Size: int64(len(sealed)), Created: now.Unix()}
	_, _ = sqlDB.Exec(`INSERT INTO cp_backups(key,size,created) VALUES(?,?,?)`, b.Key, b.Size, b.Created)

	rows, err := sqlDB.Query(`SELECT key FROM cp_backups ORDER BY created DESC`)
	if err == nil {
		var old []string
		for i := 0; rows.Next(); i++ {
			var k string
			if rows.Scan(&k) == nil && i >= cpBackupKeep {
				old = append(old, k)
			}
		}
		rows.Close()
		for _, k := range old {
			if c.Delete(context.Background(), k) == nil {
				_, _ = sqlDB.Exec(`DELETE FROM cp_backups WHERE key=?`, k)
			}
		}
	}
	return b, nil
}

func listCPBackups() []cpBackup {
	out := []cpBackup{}
	rows, err := sqlDB.Query(`SELECT key, size, created FROM cp_backups ORDER BY created DESC`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var b cpBackup
		if rows.Scan(&b.Key, &b.Size, &b.Created) == nil {
			out = append(out, b)
		}
	}
	return out
}

func (p *Panel) cpBackupLoop() {
	time.Sleep(10 * time.Minute)
	for {
		guard("controlPlaneBackup", func() {
			var last int64
			_ = sqlDB.QueryRow(`SELECT COALESCE(MAX(created),0) FROM cp_backups`).Scan(&last)
			if time.Since(time.Unix(last, 0)) < cpBackupEvery || !getOffsite(foundingTeam()).configured() {
				return
			}
			if b, err := p.runControlPlaneBackup(); err != nil {
				log.Printf("skiff: control-plane backup failed: %v", err)
			} else {
				log.Printf("skiff: control-plane backup saved to %s", b.Key)
			}
		})
		time.Sleep(time.Hour)
	}
}

func (p *Panel) handleControlPlane(w http.ResponseWriter, r *http.Request) {
	if !p.isInstanceAdmin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Query().Get("download") == "1":
		sealed, err := controlPlaneBundle()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		p.audit(r, "control-plane.download", "", "")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="skiff-`+time.Now().UTC().Format("20060102-1504")+`.skiffbak"`)
		_, _ = w.Write(sealed)
	case r.Method == http.MethodGet:
		out := map[string]any{"bucket": "", "backups": listCPBackups()}
		if t := getOffsite(foundingTeam()); t.configured() {
			out["bucket"] = t.Bucket
		}
		if r.URL.Query().Get("reveal") == "1" {
			k, err := recoveryKey()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			p.audit(r, "control-plane.reveal-key", "", "")
			out["recoveryKey"] = hex.EncodeToString(k)
		}
		writeJSON(w, out)
	case r.Method == http.MethodPost:
		b, err := p.runControlPlaneBackup()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		p.audit(r, "control-plane.backup", b.Key, "")
		writeJSON(w, b)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
