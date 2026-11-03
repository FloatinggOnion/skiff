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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nuelScript/skiff/internal/docker"
)

const (
	checkRunning = "running"
	checkPassed  = "passed"
	checkFailed  = "failed"
	checkSkipped = "skipped" // the check couldn't run, which says nothing about the backup

	restoreCheckLabel = "skiff.kind=restore-check"
	restoreCheckEvery = 23 * time.Hour   // not 24, so the daily backup's check doesn't slip later each day
	restoreCheckLimit = 15 * time.Minute // what a real restore gets
	checkMemory       = int64(1 << 30)
)

// restoreCheckMu keeps it to one check at a time, because each one runs a whole extra database.
var restoreCheckMu sync.Mutex

var backupFailAlerted = map[string]int64{}

type restoreResult struct {
	Status, Detail string
	Items          int
}

func saveRestoreCheck(id string, r restoreResult, at int64) {
	if _, err := sqlDB.Exec(`UPDATE backups SET check_status=?,check_detail=?,check_items=?,check_at=? WHERE id=?`,
		r.Status, r.Detail, r.Items, at, id); err != nil {
		log.Printf("skiff: saving restore check of backup %s: %v", id, err)
	}
}

// restoreCheckSummary says when the database's backups last restored cleanly, and whether a later check failed.
func restoreCheckSummary(dbID string) (verifiedAt int64, failed bool) {
	_ = sqlDB.QueryRow(`SELECT COALESCE(MAX(check_at), 0) FROM backups WHERE db_id=? AND check_status=?`, dbID, checkPassed).Scan(&verifiedAt)
	var failedAt int64
	_ = sqlDB.QueryRow(`SELECT COALESCE(MAX(check_at), 0) FROM backups WHERE db_id=? AND check_status=?`, dbID, checkFailed).Scan(&failedAt)
	return verifiedAt, failedAt > verifiedAt
}

// nextRestoreCheck picks the newest backup when one is due. rows are newest first. A skipped check is tried again the next day, and a backup that was checked isn't checked twice.
func nextRestoreCheck(rows []backupRow, now int64) (backupRow, bool) {
	if len(rows) == 0 {
		return backupRow{}, false
	}
	var last int64
	for _, b := range rows {
		last = max(last, b.CheckAt)
	}
	latest := rows[0]
	if now-last < int64(restoreCheckEvery/time.Second) || (latest.CheckAt != 0 && latest.CheckStatus != checkSkipped) {
		return backupRow{}, false
	}
	return latest, true
}

func checkableDB(d dbRow) bool {
	return dbEngines[d.Engine].canRestore() && dbEngines[d.Engine].countCmd != nil && d.PreviewApp == "" && !isPreviewStack(d.Stack)
}

func (p *Panel) restoreCheckLoop() {
	time.Sleep(5 * time.Minute) // a previous panel may still be finishing during a self-deploy
	p.clearRestoreChecks()
	for {
		guard("restoreCheckLoop", p.runDueRestoreChecks)
		time.Sleep(time.Hour)
	}
}

// clearRestoreChecks removes what a restart interrupted.
func (p *Panel) clearRestoreChecks() {
	restoreCheckMu.Lock()
	defer restoreCheckMu.Unlock()
	for _, name := range p.eng.ContainersLabelled(restoreCheckLabel) {
		p.removeCheckContainer(name)
	}
	_, _ = sqlDB.Exec(`UPDATE backups SET check_status='',check_detail='',check_at=0 WHERE check_status=?`, checkRunning)
}

func (p *Panel) removeCheckContainer(name string) {
	_ = p.eng.Remove(name)
	_ = p.eng.RemoveVolume(name + "-data")
}

func (p *Panel) runDueRestoreChecks() {
	for _, d := range allDatabases() {
		if !checkableDB(d) || p.eng.State(d.Container) != "running" {
			continue
		}
		rows := listBackupRows(d.ID)
		b, due := nextRestoreCheck(rows, time.Now().Unix())
		if !due {
			continue
		}
		restoreCheckMu.Lock()
		_, failedBefore := restoreCheckSummary(d.ID)
		r := p.runRestoreCheck(d, b)
		restoreCheckMu.Unlock()
		log.Printf("skiff: restore check of %s: %s %s", d.Name, r.Status, r.Detail)
		alertRestoreCheck(d, r, failedBefore)
	}
}

// runRestoreCheck always records an outcome, even if the check panics.
func (p *Panel) runRestoreCheck(d dbRow, b backupRow) restoreResult {
	r := restoreResult{Status: checkSkipped, Detail: "Skipped: the check stopped unexpectedly."}
	guard("restoreCheck", func() { r = p.checkBackup(d, b) })
	saveRestoreCheck(b.ID, r, time.Now().Unix())
	return r
}

// alertRestoreCheck leaves the reason out, because it can quote the backup's contents and alert channels are often shared.
func alertRestoreCheck(d dbRow, r restoreResult, failedBefore bool) {
	switch {
	case r.Status == checkFailed:
		go dispatchAlert(alertEvent{Team: d.Team, Kind: "backup.check_failed", App: d.Name,
			Title:  "A backup of " + d.Name + " didn't restore",
			Detail: "Skiff tried its latest backup in a throwaway database and it failed. Your database wasn't touched. Open Backups on it in the dashboard to see why."})
	case r.Status == checkPassed && failedBefore:
		go dispatchAlert(alertEvent{Team: d.Team, Kind: "backup.check_recovered", App: d.Name,
			Title: "Backups of " + d.Name + " restore again", Detail: "The latest backup restored cleanly."})
	}
}

// alertBackupFailed tells the team once a day, because the backup is retried every hour.
func (p *Panel) alertBackupFailed(d dbRow, now int64) {
	if now-backupFailAlerted[d.ID] < 24*3600 {
		return
	}
	backupFailAlerted[d.ID] = now
	go dispatchAlert(alertEvent{Team: d.Team, Kind: "backup.failed", App: d.Name,
		Title:  "Couldn't back up " + d.Name,
		Detail: "The daily backup failed, and Skiff will keep trying every hour."})
}

// checkBackup restores a backup into a throwaway database that has no network, using the same code as a real restore.
func (p *Panel) checkBackup(d dbRow, b backupRow) restoreResult {
	skipped := func(format string, a ...any) restoreResult {
		return restoreResult{Status: checkSkipped, Detail: fmt.Sprintf(format, a...)}
	}
	failed := func(format string, a ...any) restoreResult {
		return restoreResult{Status: checkFailed, Detail: trimCheckDetail(fmt.Sprintf(format, a...))}
	}
	e := dbEngines[d.Engine]
	path := backupPath(b)
	f, err := os.Open(path)
	if err != nil {
		return failed("The backup file is missing from this server.")
	}
	defer f.Close()
	if err := hasRoomFor(filepath.Dir(path), b.Size*3); err != nil {
		return skipped("Skipped: %v.", err)
	}
	memory := checkMemory
	if e.offlineRestore {
		memory += 2 * b.Size // Redis holds all of it in memory
	}
	if u := procMem(); u.Total > 0 && u.Total-u.Used < memory {
		return skipped("Skipped: this needs %d MB of free memory, and %d MB is free.", memory>>20, (u.Total-u.Used)>>20)
	}

	scratch := d
	scratch.Container = "skiff-check-" + randToken()[:12]
	scratch.Volume = scratch.Container + "-data"
	defer p.removeCheckContainer(scratch.Container)
	env, cmd := e.container(d.Username, d.Password, d.DBName)
	if d.Image != "" {
		cmd = nil
		_ = json.Unmarshal([]byte(d.Cmd), &cmd)
	}
	if _, err := p.eng.RunDatabase(docker.DBRunSpec{
		Name: scratch.Container, Image: d.image(), Network: "none",
		Volume: scratch.Volume, MountAt: e.mountAt, Env: env, Cmd: cmd,
		Memory: strconv.FormatInt(memory, 10),
		Labels: map[string]string{"skiff.kind": "restore-check", "skiff.db": d.ID, "skiff.team": d.Team},
	}); err != nil {
		return skipped("Skipped: the throwaway database couldn't start: %v", err)
	}
	startCtx, cancelStart := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancelStart()
	if err := p.waitDatabaseReady(startCtx, scratch); err != nil {
		return skipped("Skipped: the throwaway database never became ready.")
	}
	if err := p.copyRolesTo(startCtx, d, scratch); err != nil {
		return skipped("Skipped: couldn't read the database's roles: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), restoreCheckLimit)
	defer cancel()
	if err := p.loadBackup(ctx, scratch, f); err != nil {
		if p.eng.Restarts(scratch.Container) > 0 || p.eng.State(scratch.Container) != "running" {
			return skipped("Skipped: the throwaway database ran out of memory or crashed during the restore.")
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return failed("The restore didn't finish within %d minutes, which is also the limit for a real restore.", int(restoreCheckLimit/time.Minute))
		}
		return failed("The restore failed: %v", err)
	}
	if e.offlineRestore {
		if err := p.waitRedis(ctx, scratch); err != nil {
			return failed("Redis didn't start from this backup.")
		}
	}
	items, err := p.countItems(ctx, scratch)
	if err != nil {
		return failed("The restore finished, but the restored copy couldn't be read: %v", err)
	}
	return restoreVerdict(e, items, p.liveItems(d))
}

// restoreVerdict catches a backup that loads without error and holds nothing. A smaller count isn't a failure, because the database may have grown since the backup.
func restoreVerdict(e dbEngine, restored, live int) restoreResult {
	if restored == 0 && live > 0 {
		return restoreResult{Status: checkFailed, Detail: fmt.Sprintf("The backup restored, but it's empty and the database has %s.", countOf(e, live))}
	}
	return restoreResult{Status: checkPassed, Detail: "Restored " + countOf(e, restored) + ".", Items: restored}
}

func countOf(e dbEngine, n int) string {
	if n == 1 {
		return "1 " + e.countNoun
	}
	return strconv.Itoa(n) + " " + e.countNoun + "s"
}

func trimCheckDetail(s string) string {
	if len(s) > 300 {
		return strings.ToValidUTF8(s[:300], "") + "…"
	}
	return s
}

// copyRolesTo recreates the database's Postgres roles, because a dump refers to them without creating them, and a real restore finds them already there.
func (p *Panel) copyRolesTo(ctx context.Context, d, scratch dbRow) error {
	if d.Engine != "postgres" {
		return nil
	}
	var roles bytes.Buffer
	if err := p.eng.Exec(ctx, d.Container, []string{"psql", "-U", d.Username, "-d", d.DBName, "-tAc",
		"SELECT 'CREATE ROLE ' || quote_ident(rolname) || ';' FROM pg_roles WHERE rolname !~ '^pg_' AND rolname <> current_user"}, nil, &roles); err != nil {
		return err
	}
	return p.eng.Exec(ctx, scratch.Container, []string{"psql", "-U", scratch.Username, "-d", scratch.DBName, "-v", "ON_ERROR_STOP=1", "-q", "-f", "-"}, &roles, &bytes.Buffer{})
}

var redisKeysRE = regexp.MustCompile(`keys=(\d+)`)

func (p *Panel) countItems(ctx context.Context, d dbRow) (int, error) {
	e := dbEngines[d.Engine]
	var out strings.Builder
	if err := p.eng.Exec(ctx, d.Container, e.countCmd(d.Username, d.Password, d.DBName), nil, &out); err != nil {
		return 0, err
	}
	return parseItemCount(out.String())
}

func parseItemCount(out string) (int, error) {
	if strings.Contains(out, "# Keyspace") {
		total := 0
		for _, m := range redisKeysRE.FindAllStringSubmatch(out, -1) {
			n, _ := strconv.Atoi(m[1])
			total += n
		}
		return total, nil
	}
	return strconv.Atoi(strings.TrimSpace(out))
}

// liveItems returns 0 when the count can't be read, so a busy database never fails a backup's check.
func (p *Panel) liveItems(d dbRow) int {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	n, err := p.countItems(ctx, d)
	if err != nil {
		return 0
	}
	return n
}

// handleBackupCheck starts a check of one backup and returns straight away, because a large one takes minutes.
func (p *Panel) handleBackupCheck(w http.ResponseWriter, r *http.Request) {
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
	if !checkableDB(d) {
		http.Error(w, "this database's backups can't be checked", http.StatusBadRequest)
		return
	}
	if p.eng.State(d.Container) != "running" {
		http.Error(w, "the database isn't running", http.StatusBadRequest)
		return
	}
	if !restoreCheckMu.TryLock() {
		http.Error(w, "another check is running, try again in a few minutes", http.StatusConflict)
		return
	}
	saveRestoreCheck(b.ID, restoreResult{Status: checkRunning}, time.Now().Unix())
	p.audit(r, "backup.check", d.Name, "")
	go func() {
		defer restoreCheckMu.Unlock()
		p.runRestoreCheck(d, b)
	}()
	w.WriteHeader(http.StatusAccepted)
}
