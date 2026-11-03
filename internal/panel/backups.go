package panel

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const backupRetention = 10

func backupsDir() string { return filepath.Join(skiffDir(), "backups") }

type Backup struct {
	ID      string `json:"id"`
	Size    int64  `json:"size"`
	Trigger string `json:"trigger"` // manual | scheduled | pre-restore
	Created int64  `json:"created"`
	Offsite string `json:"offsite,omitempty"` // "" not sent, pending, ok or failed
}

type backupRow struct {
	ID, DbID, Team, Engine, File string
	Size                         int64
	Trigger                      string
	Created                      int64
	Offsite                      string
}

const backupCols = `id,db_id,team,engine,file,size,trigger,created,offsite`

func scanBackup(row interface{ Scan(...any) error }) (backupRow, bool) {
	var b backupRow
	if row.Scan(&b.ID, &b.DbID, &b.Team, &b.Engine, &b.File, &b.Size, &b.Trigger, &b.Created, &b.Offsite) != nil {
		return backupRow{}, false
	}
	return b, true
}

func getBackup(id string) (backupRow, bool) {
	return scanBackup(sqlDB.QueryRow(`SELECT `+backupCols+` FROM backups WHERE id=?`, id))
}

func listBackupRows(dbID string) []backupRow {
	rows, err := sqlDB.Query(`SELECT `+backupCols+` FROM backups WHERE db_id=? ORDER BY created DESC`, dbID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []backupRow
	for rows.Next() {
		if b, ok := scanBackup(rows); ok {
			out = append(out, b)
		}
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

func putBackup(b backupRow) error {
	_, err := sqlDB.Exec(`INSERT INTO backups(`+backupCols+`) VALUES(?,?,?,?,?,?,?,?,?)`,
		b.ID, b.DbID, b.Team, b.Engine, b.File, b.Size, b.Trigger, b.Created, b.Offsite)
	return err
}

func backupPath(b backupRow) string { return filepath.Join(backupsDir(), b.DbID, b.File) }

func deleteBackup(b backupRow) {
	_ = os.Remove(backupPath(b))
	_, _ = sqlDB.Exec(`DELETE FROM backups WHERE id=?`, b.ID)
}

// removeDatabaseBackups deletes a deleted database's backups from this server, which are unencrypted copies of its data. Copies in the team's bucket stay.
func removeDatabaseBackups(dbID string) {
	for _, b := range listBackupRows(dbID) {
		deleteBackup(b)
	}
	_ = os.Remove(filepath.Join(backupsDir(), dbID))
	_, _ = sqlDB.Exec(`DELETE FROM offsite_objects WHERE db_id=?`, dbID)
}

func pruneBackups(dbID string, keep int) {
	all := listBackupRows(dbID)
	for i := keep; i < len(all); i++ {
		deleteBackup(all[i])
	}
}

func lastBackupAt(dbID string) int64 {
	var t int64
	_ = sqlDB.QueryRow(`SELECT COALESCE(MAX(created), 0) FROM backups WHERE db_id=?`, dbID).Scan(&t)
	return t
}

func allDatabases() []dbRow {
	rows, err := sqlDB.Query(`SELECT ` + dbCols + ` FROM databases`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []dbRow
	for rows.Next() {
		if d, ok := scanDB(rows); ok {
			out = append(out, d)
		}
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

func (p *Panel) runBackup(d dbRow, trigger string) (backupRow, error) {
	e := dbEngines[d.Engine]
	if e.backupExt == "" {
		return backupRow{}, fmt.Errorf("backups aren't supported for %s yet", e.label)
	}
	if p.eng.State(d.Container) != "running" {
		return backupRow{}, fmt.Errorf("the database isn't running")
	}
	id := randToken()[:12]
	dir := filepath.Join(backupsDir(), d.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return backupRow{}, err
	}
	path := filepath.Join(dir, id+e.backupExt)
	f, err := os.Create(path)
	if err != nil {
		return backupRow{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	derr := p.eng.Exec(ctx, d.Container, e.dumpCmd(d.Username, d.Password, d.DBName), nil, f)
	_ = f.Close()
	if derr != nil {
		_ = os.Remove(path)
		return backupRow{}, derr
	}
	var size int64
	if info, err := os.Stat(path); err == nil {
		size = info.Size()
	}
	b := backupRow{
		ID: id, DbID: d.ID, Team: d.Team, Engine: d.Engine, File: id + e.backupExt,
		Size: size, Trigger: trigger, Created: time.Now().Unix(),
	}
	if err := putBackup(b); err != nil {
		_ = os.Remove(path)
		return backupRow{}, err
	}
	pruneBackups(d.ID, backupRetention)
	if getOffsite(d.Team).configured() {
		b.Offsite = "pending"
		setBackupOffsite(b.ID, b.Offsite)
		uploadOffsiteAsync(d, b)
	}
	return b, nil
}

// loadBackup replaces the database's contents with a dump.
func (p *Panel) loadBackup(ctx context.Context, d dbRow, dump io.Reader) error {
	e := dbEngines[d.Engine]
	if e.offlineRestore {
		return p.restoreRedis(ctx, d, dump)
	}
	return p.eng.Exec(ctx, d.Container, e.restoreCmd(d.Username, d.Password, d.DBName),
		io.MultiReader(strings.NewReader(e.restoreReset), dump), io.Discard)
}

func toBackup(b backupRow) Backup {
	return Backup{ID: b.ID, Size: b.Size, Trigger: b.Trigger, Created: b.Created, Offsite: b.Offsite}
}

func (p *Panel) handleBackups(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		d, ok := p.canAccessDB(r, sanitizeID(r.URL.Query().Get("db")))
		if !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		out := []Backup{}
		for _, b := range listBackupRows(d.ID) {
			out = append(out, toBackup(b))
		}
		writeJSON(w, out)

	case http.MethodPost:
		d, ok := p.canAccessDB(r, sanitizeID(r.URL.Query().Get("db")))
		if !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		b, err := p.runBackup(d, "manual")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, toBackup(b))

	case http.MethodDelete:
		b, ok := getBackup(sanitizeID(r.URL.Query().Get("id")))
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if _, ok := p.canAccessDB(r, b.DbID); !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		deleteBackup(b)
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (p *Panel) handleBackupRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b, ok := getBackup(sanitizeID(r.URL.Query().Get("id")))
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	d, ok := p.canAccessDB(r, b.DbID)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	e := dbEngines[d.Engine]
	if !e.canRestore() {
		http.Error(w, "restore isn't supported for this database", http.StatusBadRequest)
		return
	}
	if p.eng.State(d.Container) != "running" {
		http.Error(w, "the database isn't running", http.StatusBadRequest)
		return
	}
	f, err := os.Open(backupPath(b))
	if err != nil {
		http.Error(w, "backup file is missing", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	// Keep what's there now, so restoring the wrong backup can be undone. The file above stays readable even if this prunes it.
	if _, err := p.runBackup(d, "pre-restore"); err != nil {
		http.Error(w, "couldn't back up the current data first, so nothing was changed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	if err := p.loadBackup(ctx, d, f); err != nil {
		http.Error(w, "restore failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	p.audit(r, "backup.restore", d.Name, "")
	w.WriteHeader(http.StatusNoContent)
}

func (p *Panel) handleBackupDownload(w http.ResponseWriter, r *http.Request) {
	b, ok := getBackup(sanitizeID(r.URL.Query().Get("id")))
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	d, ok := p.canAccessDB(r, b.DbID)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	f, err := os.Open(backupPath(b))
	if err != nil {
		http.Error(w, "backup file is missing", http.StatusNotFound)
		return
	}
	defer f.Close()
	name := fmt.Sprintf("%s-%d%s", d.Name, b.Created, dbEngines[d.Engine].backupExt)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	if b.Size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(b.Size, 10))
	}
	_, _ = io.Copy(w, f)
}

func (p *Panel) backupLoop() {
	time.Sleep(2 * time.Minute) // let startup settle before the first pass
	for {
		guard("backupLoop", func() {
			now := time.Now().Unix()
			for _, d := range allDatabases() {
				if dbEngines[d.Engine].backupExt == "" || d.PreviewApp != "" || isPreviewStack(d.Stack) {
					continue
				}
				if now-lastBackupAt(d.ID) < 24*3600 {
					continue
				}
				if p.eng.State(d.Container) != "running" {
					continue
				}
				if _, err := p.runBackup(d, "scheduled"); err != nil {
					log.Printf("skiff: scheduled backup of %s failed: %v", d.Name, err)
				}
			}
		})
		time.Sleep(time.Hour)
	}
}
