package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/nuelScript/skiff/internal/docker"
)

const (
	pitrMount     = "/skiff-wal"
	pitrBaseEvery = int64(24 * 3600)
	pitrKeepBases = 3
	pitrReserve   = int64(1 << 30) // free disk kept on top of the usual reserve; below it, archiving stops
	pitrLimit     = 15 * time.Minute
)

// pitrMu keeps it to one base backup or recovery at a time.
var pitrMu sync.Mutex

var walFileRE = regexp.MustCompile(`^[0-9A-F]{24}$`)

func pitrVolume(d dbRow) string { return d.Container + "-wal" }

func pitrEnabled(id string) bool {
	var n int
	return sqlDB.QueryRow(`SELECT 1 FROM pitr WHERE db_id=?`, id).Scan(&n) == nil
}

// pitrArchiveCommand compresses each segment, so one that's only partly full costs almost nothing. Postgres keeps every segment until this succeeds, so the PAUSED file lets it carry on when the disk is low.
const pitrArchiveCommand = "test -f " + pitrMount + "/PAUSED || test -f " + pitrMount + "/%f.gz || { gzip -c %p > " + pitrMount + "/%f.tmp && mv " + pitrMount + "/%f.tmp " + pitrMount + "/%f.gz; }"

func pitrServerArgs() []string {
	return []string{"-c", "wal_level=replica", "-c", "archive_mode=on", "-c", "archive_timeout=60", "-c", "archive_command=" + pitrArchiveCommand}
}

func withPITRArgs(cmd []string) []string {
	if len(cmd) == 0 {
		cmd = []string{"postgres"}
	}
	return append(append([]string{}, cmd...), pitrServerArgs()...)
}

// pitrRunArgs adds what a database with recovery on needs to its container's command and mounts.
func (p *Panel) pitrRunArgs(d dbRow, cmd []string) ([]string, []string, error) {
	if !pitrEnabled(d.ID) {
		return cmd, nil, nil
	}
	if err := p.prepareWALVolume(d); err != nil {
		return nil, nil, err
	}
	return withPITRArgs(cmd), []string{pitrVolume(d) + ":" + pitrMount}, nil
}

func (p *Panel) prepareWALVolume(d dbRow) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	return p.inWALVolume(ctx, d, "chown postgres:postgres "+pitrMount)
}

func (p *Panel) inWALVolume(ctx context.Context, d dbRow, script string) error {
	return p.eng.RunIO(ctx, d.image(), []docker.Mount{{Name: pitrVolume(d), Destination: pitrMount}}, script, nil, &bytes.Buffer{})
}

type pitrBase struct {
	ID, DbID, File, WAL string
	Size, Created       int64
}

// listPITRBases returns them oldest first.
func listPITRBases(dbID string) []pitrBase {
	rows, err := sqlDB.Query(`SELECT id,db_id,file,size,wal,created FROM pitr_bases WHERE db_id=? ORDER BY created, id`, dbID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []pitrBase
	for rows.Next() {
		var b pitrBase
		if rows.Scan(&b.ID, &b.DbID, &b.File, &b.Size, &b.WAL, &b.Created) == nil {
			out = append(out, b)
		}
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

func pitrBasePath(b pitrBase) string { return filepath.Join(backupsDir(), b.DbID, b.File) }

func deletePITRBase(b pitrBase) {
	_ = os.Remove(pitrBasePath(b))
	_, _ = sqlDB.Exec(`DELETE FROM pitr_bases WHERE id=?`, b.ID)
}

// baseFor picks the newest base backup finished by the target time. bases are oldest first.
func baseFor(bases []pitrBase, at int64) (pitrBase, bool) {
	var found pitrBase
	ok := false
	for _, b := range bases {
		if b.Created <= at {
			found, ok = b, true
		}
	}
	return found, ok
}

func (p *Panel) pitrLoop() {
	time.Sleep(3 * time.Minute) // a previous panel may still be finishing during a self-deploy
	for {
		guard("pitrLoop", p.pitrTick)
		time.Sleep(time.Minute)
	}
}

func (p *Panel) pitrTick() {
	rows, err := sqlDB.Query(`SELECT db_id FROM pitr`)
	if err != nil {
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		d, ok := getDB(id)
		if ok && p.eng.State(d.Container) == "running" {
			p.tendPITR(d)
		}
	}
}

func (p *Panel) tendPITR(d dbRow) {
	pitrMu.Lock()
	defer pitrMu.Unlock()
	dir := filepath.Join(backupsDir(), d.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	paused := pitrPaused(d.ID) != ""
	if err := hasRoomFor(dir, pitrReserve); err != nil && !paused {
		p.pausePITR(d, err)
		return
	}
	if paused {
		if hasRoomFor(dir, 2*pitrReserve) != nil {
			return
		}
		if err := p.resumePITR(d); err != nil {
			log.Printf("skiff: resuming recovery of %s: %v", d.Name, err)
			return
		}
	}
	bases := listPITRBases(d.ID)
	for _, b := range bases {
		if _, err := os.Stat(pitrBasePath(b)); err != nil {
			deletePITRBase(b)
		}
	}
	bases = listPITRBases(d.ID)
	if len(bases) == 0 || time.Now().Unix()-bases[len(bases)-1].Created >= pitrBaseEvery {
		if err := p.takePITRBase(d); err != nil {
			log.Printf("skiff: base backup of %s failed: %v", d.Name, err)
			return
		}
		p.prunePITR(d)
	}
}

func pitrPaused(id string) string {
	var reason string
	_ = sqlDB.QueryRow(`SELECT paused FROM pitr WHERE db_id=?`, id).Scan(&reason)
	return reason
}

// pausePITR gives up the history rather than the server: a gap would make everything after it unusable anyway.
func (p *Panel) pausePITR(d dbRow, why error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := p.eng.Exec(ctx, d.Container, []string{"touch", pitrMount + "/PAUSED"}, nil, &bytes.Buffer{}); err != nil {
		log.Printf("skiff: pausing recovery of %s: %v", d.Name, err)
		return
	}
	for _, b := range listPITRBases(d.ID) {
		deletePITRBase(b)
	}
	_ = p.eng.Exec(ctx, d.Container, []string{"sh", "-c", "rm -f " + pitrMount + "/*.gz " + pitrMount + "/*.tmp"}, nil, &bytes.Buffer{})
	reason := "Paused because the server is low on disk space: " + why.Error()
	_, _ = sqlDB.Exec(`UPDATE pitr SET paused=? WHERE db_id=?`, reason, d.ID)
	go dispatchAlert(alertEvent{Team: d.Team, Kind: "database.recovery_paused", App: d.Name,
		Title:  "Point-in-time recovery paused for " + d.Name,
		Detail: "The server is low on disk space, so Skiff stopped keeping its history and cleared it. Daily backups still run. It starts again by itself when there's room."})
}

func (p *Panel) resumePITR(d dbRow) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := p.eng.Exec(ctx, d.Container, []string{"rm", "-f", pitrMount + "/PAUSED"}, nil, &bytes.Buffer{}); err != nil {
		return err
	}
	_, err := sqlDB.Exec(`UPDATE pitr SET paused='' WHERE db_id=?`, d.ID)
	return err
}

// takePITRBase copies the database's files while it runs. The copy is only usable together with the history archived since it began.
func (p *Panel) takePITRBase(d dbRow) error {
	ctx, cancel := context.WithTimeout(context.Background(), pitrLimit)
	defer cancel()
	id := randToken()[:12]
	b := pitrBase{ID: id, DbID: d.ID, File: "base-" + id + ".tar.gz"}
	path := pitrBasePath(b)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	err = p.eng.Exec(ctx, d.Container, []string{"pg_basebackup", "-U", d.Username, "-D", "-", "-Ft", "-z", "-X", "none", "--checkpoint=fast", "--no-manifest"}, nil, f)
	_ = f.Close()
	if err != nil {
		_ = os.Remove(path)
		return err
	}
	if info, err := os.Stat(path); err == nil {
		b.Size = info.Size()
	}
	var out strings.Builder
	_ = p.eng.Exec(ctx, d.Container, []string{"sh", "-c", "ls -t " + pitrMount + "/*.backup.gz 2>/dev/null | head -1"}, nil, &out)
	b.WAL = strings.TrimSuffix(filepath.Base(strings.TrimSpace(out.String())), ".gz")
	b.Created = time.Now().Unix()
	if _, err := sqlDB.Exec(`INSERT INTO pitr_bases(id,db_id,file,size,wal,created) VALUES(?,?,?,?,?,?)`, b.ID, b.DbID, b.File, b.Size, b.WAL, b.Created); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

// prunePITR drops old base backups and the history only they could use.
func (p *Panel) prunePITR(d dbRow) {
	bases := listPITRBases(d.ID)
	for len(bases) > pitrKeepBases {
		deletePITRBase(bases[0])
		bases = bases[1:]
	}
	if len(bases) == 0 || bases[0].WAL == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := p.eng.Exec(ctx, d.Container, []string{"pg_archivecleanup", "-x", ".gz", pitrMount, bases[0].WAL}, nil, &bytes.Buffer{}); err != nil {
		log.Printf("skiff: cleaning history of %s: %v", d.Name, err)
	}
}

func (p *Panel) enablePITR(d dbRow) error {
	if _, err := sqlDB.Exec(`INSERT OR IGNORE INTO pitr(db_id,enabled) VALUES(?,?)`, d.ID, time.Now().Unix()); err != nil {
		return err
	}
	if err := p.rerunDatabase(d); err != nil {
		_, _ = sqlDB.Exec(`DELETE FROM pitr WHERE db_id=?`, d.ID)
		_ = p.rerunDatabase(d)
		return err
	}
	return nil
}

// startPITRHistory takes the first base backup straight away, so recovery is usable without waiting for the next pass.
func (p *Panel) startPITRHistory(d dbRow) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := p.waitDatabaseReady(ctx, d); err == nil {
		p.tendPITR(d)
	}
}

func (p *Panel) disablePITR(d dbRow) error {
	if _, err := sqlDB.Exec(`DELETE FROM pitr WHERE db_id=?`, d.ID); err != nil {
		return err
	}
	if err := p.rerunDatabase(d); err != nil {
		return err
	}
	p.removePITR(d)
	return nil
}

// removePITR deletes everything kept for recovery.
func (p *Panel) removePITR(d dbRow) {
	for _, b := range listPITRBases(d.ID) {
		deletePITRBase(b)
	}
	_, _ = sqlDB.Exec(`DELETE FROM pitr WHERE db_id=?`, d.ID)
	_ = p.eng.RemoveVolume(pitrVolume(d))
}

// resetPITR starts the history again, for when the data it describes has been replaced.
func (p *Panel) resetPITR(d dbRow) {
	if !pitrEnabled(d.ID) {
		return
	}
	for _, b := range listPITRBases(d.ID) {
		deletePITRBase(b)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := p.eng.Exec(ctx, d.Container, []string{"sh", "-c", "rm -f " + pitrMount + "/*"}, nil, &bytes.Buffer{}); err != nil {
		log.Printf("skiff: resetting recovery of %s: %v", d.Name, err)
	}
}

// rerunDatabase replaces the container with the same data, networks and public access.
func (p *Panel) rerunDatabase(d dbRow) error {
	_, err := p.runDatabaseContainer(d, d.Public)
	return err
}

// recoverToTime proves a recovered copy starts before the database is touched: it recovers into a new volume while the database keeps running, then swaps the volumes.
func (p *Panel) recoverToTime(d dbRow, at int64) error {
	base, ok := baseFor(listPITRBases(d.ID), at)
	if !ok {
		return errors.New("that time is before the history Skiff has for this database")
	}
	path := pitrBasePath(base)
	f, err := os.Open(path)
	if err != nil {
		return errors.New("its base backup is missing from this server")
	}
	defer f.Close()
	if err := hasRoomFor(filepath.Dir(path), base.Size*5); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), pitrLimit)
	defer cancel()
	if err := p.flushHistory(ctx, d); err != nil {
		return fmt.Errorf("couldn't archive the latest history: %w", err)
	}

	e := dbEngines[d.Engine]
	suffix := randToken()[:8]
	volume := d.Container + "-data-" + suffix
	scratch := d
	scratch.Container = "skiff-check-" + suffix
	scratch.Volume = volume
	swapped := false
	defer func() {
		_ = p.eng.Remove(scratch.Container)
		if !swapped {
			_ = p.eng.RemoveVolume(volume)
		}
	}()
	target := time.Unix(at, 0).UTC().Format("2006-01-02 15:04:05+00")
	unpack := fmt.Sprintf(`set -e; cd %s; tar xzf -; chown -R postgres:postgres .; chmod 700 .; touch recovery.signal
cat >> postgresql.auto.conf <<'EOF'
restore_command = 'gunzip -c %s/%%f.gz > %%p'
recovery_target_time = '%s'
recovery_target_action = 'promote'
EOF`, e.mountAt, pitrMount, target)
	if err := p.eng.RunIO(ctx, d.image(), []docker.Mount{{Name: volume, Destination: e.mountAt}}, unpack, f, &bytes.Buffer{}); err != nil {
		return fmt.Errorf("unpacking the base backup: %w", err)
	}
	env, _ := e.container(d.Username, d.Password, d.DBName)
	if _, err := p.eng.RunDatabase(docker.DBRunSpec{
		Name: scratch.Container, Image: d.image(), Network: "none",
		Volume: volume, MountAt: e.mountAt, Env: env,
		Binds:  []string{pitrVolume(d) + ":" + pitrMount + ":ro"},
		Labels: map[string]string{"skiff.kind": "restore-check", "skiff.db": d.ID, "skiff.team": d.Team},
	}); err != nil {
		return fmt.Errorf("starting the recovered copy: %w", err)
	}
	if err := p.waitRecovered(ctx, scratch); err != nil {
		return err
	}
	if err := p.eng.Stop(scratch.Container); err != nil {
		return fmt.Errorf("stopping the recovered copy: %w", err)
	}

	// Writes after this backup would otherwise be lost for good.
	if _, err := p.runBackup(d, "pre-restore"); err != nil {
		return fmt.Errorf("couldn't back up the current data first, so nothing was changed: %w", err)
	}
	old := d.volume()
	if err := p.useVolume(&d, volume); err != nil {
		return err
	}
	_ = p.eng.Stop(d.Container)
	if err := p.rerunDatabase(d); err != nil {
		_ = p.useVolume(&d, old)
		_ = p.rerunDatabase(d)
		return fmt.Errorf("the database didn't start on the recovered data, so it went back: %w", err)
	}
	if err := p.waitDatabaseReady(ctx, d); err != nil {
		_ = p.useVolume(&d, old)
		_ = p.rerunDatabase(d)
		return fmt.Errorf("the database didn't start on the recovered data, so it went back: %w", err)
	}
	swapped = true
	_ = p.eng.RemoveVolume(old)
	for _, b := range listPITRBases(d.ID) {
		deletePITRBase(b)
	}
	if err := p.clearHistory(ctx, d); err != nil {
		log.Printf("skiff: clearing history of %s: %v", d.Name, err)
	}
	_, _ = sqlDB.Exec(`UPDATE pitr SET paused='' WHERE db_id=?`, d.ID)
	if err := p.takePITRBase(d); err != nil {
		log.Printf("skiff: base backup of %s after recovery failed: %v", d.Name, err)
	}
	return nil
}

func (p *Panel) useVolume(d *dbRow, volume string) error {
	d.Volume = volume
	_, err := sqlDB.Exec(`UPDATE databases SET volume=? WHERE id=?`, volume, d.ID)
	return err
}

// clearHistory drops the old segments, which describe data that's gone. It's only safe once the recovered database is up, and timeline files stay because recovery reads them.
func (p *Panel) clearHistory(ctx context.Context, d dbRow) error {
	return p.eng.Exec(ctx, d.Container, []string{"find", pitrMount, "-type", "f", "!", "-name", "*.history*", "-delete"}, nil, &bytes.Buffer{})
}

// flushHistory makes recovery able to reach any time up to now: it needs a commit after the target, and the segment holding it archived.
func (p *Panel) flushHistory(ctx context.Context, d dbRow) error {
	psql := func(q string) (string, error) {
		var out strings.Builder
		err := p.eng.Exec(ctx, d.Container, []string{"psql", "-U", d.Username, "-d", d.DBName, "-tAc", q}, nil, &out)
		return strings.TrimSpace(out.String()), err
	}
	if _, err := psql("SELECT txid_current()"); err != nil {
		return err
	}
	segment, err := psql("SELECT pg_walfile_name(pg_switch_wal())")
	if err != nil {
		return err
	}
	if !walFileRE.MatchString(segment) {
		return fmt.Errorf("unexpected segment name %q", segment)
	}
	for {
		if p.eng.Exec(ctx, d.Container, []string{"test", "-f", pitrMount + "/" + segment + ".gz"}, nil, &bytes.Buffer{}) == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func (p *Panel) waitRecovered(ctx context.Context, d dbRow) error {
	for {
		var out strings.Builder
		err := p.eng.Exec(ctx, d.Container, []string{"psql", "-U", d.Username, "-d", d.DBName, "-tAc", "SELECT pg_is_in_recovery()"}, nil, &out)
		if err == nil && strings.TrimSpace(out.String()) == "f" {
			return nil
		}
		if p.eng.Restarts(d.Container) > 2 {
			var logs bytes.Buffer
			_ = p.eng.Logs(d.Container, false, "5", &logs)
			log.Printf("skiff: recovered copy of %s kept crashing: %s", d.Name, logs.String())
			return errors.New("the recovered copy wouldn't start, so nothing was changed")
		}
		select {
		case <-ctx.Done():
			return errors.New("recovery didn't finish in time, so nothing was changed")
		case <-time.After(time.Second):
		}
	}
}

type pitrStatus struct {
	Supported   bool   `json:"supported"`
	Enabled     bool   `json:"enabled"`
	Paused      string `json:"paused,omitempty"`
	WindowStart int64  `json:"windowStart,omitempty"`
	Size        int64  `json:"size,omitempty"`
}

func pitrSupported(d dbRow) bool {
	return d.Engine == "postgres" && d.Image == "" && d.PreviewApp == "" && !isPreviewStack(d.Stack)
}

func getPITRStatus(d dbRow) pitrStatus {
	s := pitrStatus{Supported: pitrSupported(d), Enabled: pitrEnabled(d.ID), Paused: pitrPaused(d.ID)}
	bases := listPITRBases(d.ID)
	if len(bases) > 0 {
		s.WindowStart = bases[0].Created
	}
	for _, b := range bases {
		s.Size += b.Size
	}
	return s
}

func (p *Panel) handlePITR(w http.ResponseWriter, r *http.Request) {
	d, ok := p.canAccessDB(r, sanitizeID(r.URL.Query().Get("db")))
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, getPITRStatus(d))
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !pitrSupported(d) {
		http.Error(w, "point-in-time recovery only works with Skiff's own Postgres databases", http.StatusBadRequest)
		return
	}
	if p.eng.State(d.Container) != "running" {
		http.Error(w, "the database isn't running", http.StatusBadRequest)
		return
	}
	if !pitrMu.TryLock() {
		http.Error(w, "another base backup or recovery is running, try again in a few minutes", http.StatusConflict)
		return
	}
	defer pitrMu.Unlock()
	var body struct {
		Action string `json:"action"`
		At     int64  `json:"at"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var err error
	switch body.Action {
	case "enable":
		if !pitrEnabled(d.ID) {
			if err = p.enablePITR(d); err == nil {
				p.audit(r, "database.recovery_on", d.Name, "")
				go guard("firstBase", func() { p.startPITRHistory(d) })
			}
		}
	case "disable":
		if pitrEnabled(d.ID) {
			if err = p.disablePITR(d); err == nil {
				p.audit(r, "database.recovery_off", d.Name, "")
			}
		}
	case "restore":
		switch {
		case !pitrEnabled(d.ID):
			http.Error(w, "point-in-time recovery isn't on for this database", http.StatusBadRequest)
			return
		case body.At <= 0 || body.At >= time.Now().Unix():
			http.Error(w, "choose a time in the past", http.StatusBadRequest)
			return
		}
		p.audit(r, "database.recover", d.Name, time.Unix(body.At, 0).UTC().Format(time.RFC3339))
		err = p.recoverToTime(d, body.At)
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d, _ = getDB(d.ID)
	writeJSON(w, getPITRStatus(d))
}
