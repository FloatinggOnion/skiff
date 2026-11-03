package panel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/nuelScript/skiff/internal/docker"
)

const (
	previewDataEmpty = "empty"
	previewDataCopy  = "copy"
)

type dbAttachment struct{ ID, Var string }

func appDatabaseAttachments(app string) []dbAttachment {
	rows, err := sqlDB.Query(`SELECT db_id, var FROM db_attachments WHERE app=? ORDER BY db_id`, app)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []dbAttachment
	for rows.Next() {
		var a dbAttachment
		if rows.Scan(&a.ID, &a.Var) == nil {
			out = append(out, a)
		}
	}
	return out
}

func previewDatabases(app string) []dbRow {
	rows, err := sqlDB.Query(`SELECT `+dbCols+` FROM databases WHERE preview_app=?`, app)
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
	return out
}

func (p *Panel) removePreviewDatabases(app string) {
	for _, d := range previewDatabases(app) {
		p.removeStackDB(d)
	}
}

// preparePreviewData gives a preview its own copy of each database its project uses. An error stops the deploy, because a preview that carried on would inherit the project's connection and reach its real data.
func (p *Panel) preparePreviewData(ctx context.Context, src Source, logln func(string)) error {
	if src.Parent == "" {
		return nil
	}
	parent, ok := getSource(src.Parent)
	if !ok || parent.PreviewData == "" {
		return nil
	}
	have := map[string]bool{}
	for _, a := range appDatabaseAttachments(src.App) {
		have[a.Var] = true
	}
	for _, a := range appDatabaseAttachments(parent.App) {
		if have[a.Var] {
			continue
		}
		source, ok := getDB(a.ID)
		if !ok {
			return fmt.Errorf("the project's database for %s is missing", a.Var)
		}
		if err := p.makePreviewDatabase(ctx, src, parent.PreviewData, source, a.Var, logln); err != nil {
			return fmt.Errorf("%s: %w", source.Name, err)
		}
	}
	return nil
}

func (p *Panel) makePreviewDatabase(ctx context.Context, src Source, mode string, source dbRow, envVar string, logln func(string)) error {
	var dump *os.File
	if mode == previewDataCopy {
		b, err := p.latestBackup(source, logln)
		if err != nil {
			return err
		}
		path := backupPath(b)
		// Restoring needs room for the data and for the database's own overhead.
		if err := hasRoomFor(filepath.Dir(path), b.Size*3); err != nil {
			return err
		}
		if dump, err = os.Open(path); err != nil {
			return fmt.Errorf("reading its backup: %w", err)
		}
		defer dump.Close()
	}

	e := dbEngines[source.Engine]
	id := randToken()[:12]
	container := "skiff-db-" + id
	pass := randToken()
	if source.Engine == "redis" && source.Password == "" {
		pass = ""
	}
	dbname := ""
	if e.hasDB {
		dbname = source.DBName
	}
	env, cmd := e.container(source.Username, pass, dbname)
	net := teamNetwork(src.Team)
	if err := p.eng.EnsureNetwork(net); err != nil {
		return err
	}
	logln("→ starting a separate " + e.label + " database for this preview")
	if _, err := p.eng.RunDatabase(docker.DBRunSpec{
		Name: container, Image: source.image(), Network: net, Volume: container + "-data", MountAt: e.mountAt, Env: env, Cmd: cmd,
		Labels: map[string]string{"skiff.kind": "database", "skiff.db": id, "skiff.team": src.Team, "skiff.preview": src.App},
	}); err != nil {
		return err
	}
	rawCmd, _ := json.Marshal(cmd)
	d := dbRow{
		ID: id, Team: src.Team, Name: src.App + "-" + source.Name, Engine: source.Engine, Container: container, Host: container, Port: e.port,
		Username: source.Username, Password: pass, DBName: dbname, Created: time.Now().Unix(),
		Image: source.image(), Volume: container + "-data", Cmd: string(rawCmd), PreviewApp: src.App,
	}
	if err := putDB(d); err != nil {
		_ = p.eng.Remove(container)
		_ = p.eng.RemoveVolume(d.Volume)
		return err
	}
	fail := func(err error) error {
		p.removeStackDB(d)
		return err
	}
	readyCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	if err := p.waitDatabaseReady(readyCtx, d); err != nil {
		return fail(err)
	}
	if dump != nil {
		logln("→ loading a copy of its latest backup")
		if err := p.loadBackup(ctx, d, dump); err != nil {
			return fail(fmt.Errorf("loading its backup: %w", err))
		}
	}
	if err := upsertEnvVar(src.App, envVar, e.url(d.Host, d.Port, d.Username, d.Password, d.DBName, false), false); err != nil {
		return fail(err)
	}
	if err := attachDB(d.ID, src.App, envVar); err != nil {
		return fail(err)
	}
	return nil
}

// latestBackup returns the source database's newest backup, taking one if it has none.
func (p *Panel) latestBackup(d dbRow, logln func(string)) (backupRow, error) {
	if e := dbEngines[d.Engine]; e.backupExt == "" {
		return backupRow{}, fmt.Errorf("%s can't be copied", e.label)
	}
	if rows := listBackupRows(d.ID); len(rows) > 0 {
		return rows[0], nil
	}
	logln("→ taking its first backup")
	b, err := p.runBackup(d, "manual")
	if err != nil {
		return backupRow{}, fmt.Errorf("it has no backup to copy, and one couldn't be taken: %w", err)
	}
	return b, nil
}

func (p *Panel) handlePreviewData(w http.ResponseWriter, r *http.Request) {
	app := sanitizeName(r.URL.Query().Get("app"))
	src, ok := getSource(app)
	if !ok || !p.canAccess(r, app) || src.Parent != "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
		names := []string{}
		for _, a := range appDatabaseAttachments(app) {
			if d, ok := getDB(a.ID); ok {
				names = append(names, d.Name)
			}
		}
		writeJSON(w, map[string]any{"mode": src.PreviewData, "databases": names})
	case http.MethodPut:
		if !p.isOwner(r) {
			http.Error(w, "only a team owner can change this", http.StatusForbidden)
			return
		}
		var body struct {
			Mode string `json:"mode"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body) != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if body.Mode != "" && body.Mode != previewDataEmpty && body.Mode != previewDataCopy {
			http.Error(w, "unknown mode", http.StatusBadRequest)
			return
		}
		src.PreviewData = body.Mode
		if err := putSource(src); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		p.audit(r, "project.preview-data", app, body.Mode)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
