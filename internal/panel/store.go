package panel

import (
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type Source struct {
	App         string `json:"app"`
	Team        string `json:"team"`
	Repo        string `json:"repo"`
	Branch      string `json:"branch"`
	RootDir     string `json:"rootDir"`
	Port        string `json:"port"`
	CloneURL    string `json:"cloneUrl"`
	Auto        bool   `json:"auto"`
	Parent      string `json:"parent,omitempty"`
	PreviewAuto bool   `json:"previewAuto,omitempty"`
	Replicas    int    `json:"replicas,omitempty"`
	Release     string `json:"release,omitempty"`
	Autoscale   bool   `json:"autoscale,omitempty"`
	ScaleMin    int    `json:"scaleMin,omitempty"`
	ScaleMax    int    `json:"scaleMax,omitempty"`
	ScaleCPU    int    `json:"scaleCpu,omitempty"`
	Origin      string `json:"origin,omitempty"`     // "" = deployed by the panel from git, "cli" = by `skiff deploy`
	SleepAfter  int    `json:"sleepAfter,omitempty"` // minutes without visitors before the app is stopped; 0 = never
	// HealthPath must answer 2xx/3xx before a new version takes traffic; HealthTimeout is in seconds (0 = default).
	HealthPath    string `json:"healthPath,omitempty"`
	HealthTimeout int    `json:"healthTimeout,omitempty"`
	// Image is a prebuilt image the project runs instead of building from a repo.
	Image string `json:"image,omitempty"`
	// Memory and CPU limit each replica, like 512m and 0.5; empty leaves it to the server-wide safety cap.
	Memory string `json:"memory,omitempty"`
	CPU    string `json:"cpu,omitempty"`
	// Catalog is the one-click recipe this project was created from.
	Catalog string `json:"catalog,omitempty"`
	// Private means no public address; other services on the team's network still reach it by name.
	Private bool `json:"private,omitempty"`
	// RateLimit is requests per minute per visitor at the router; 0 means none.
	RateLimit int `json:"rateLimit,omitempty"`
}

type Deploy struct {
	ID           string `json:"id"`
	App          string `json:"app"`
	Commit       string `json:"commit"`
	Message      string `json:"message"`
	Trigger      string `json:"trigger"`
	Status       string `json:"status"`
	Started      int64  `json:"started"`
	Rollbackable bool   `json:"rollbackable,omitempty"`
}

type EnvVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Build bool   `json:"build"`
}

var sqlDB *sql.DB

func skiffDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".skiff")
}

func logPath(app, id string) string {
	return filepath.Join(skiffDir(), "deploys", sanitizeName(app), sanitizeID(id)+".log")
}

const sourceCols = `app,team,repo,branch,root_dir,port,clone_url,auto,parent,preview_auto,replicas,release,autoscale,scale_min,scale_max,scale_cpu,origin,sleep_after,health_path,health_timeout,image,memory_limit,cpu_limit,catalog,is_private,rate_limit`

func putSource(s Source) error {
	if s.Replicas < 1 {
		s.Replicas = 1
	}
	_, err := sqlDB.Exec(`
		INSERT INTO sources(`+sourceCols+`)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(app) DO UPDATE SET
			team=excluded.team, repo=excluded.repo, branch=excluded.branch,
			root_dir=excluded.root_dir, port=excluded.port,
			clone_url=excluded.clone_url, auto=excluded.auto, parent=excluded.parent,
			preview_auto=excluded.preview_auto, replicas=excluded.replicas, release=excluded.release,
			autoscale=excluded.autoscale, scale_min=excluded.scale_min, scale_max=excluded.scale_max, scale_cpu=excluded.scale_cpu,
			origin=excluded.origin, sleep_after=excluded.sleep_after,
			health_path=excluded.health_path, health_timeout=excluded.health_timeout, image=excluded.image,
			memory_limit=excluded.memory_limit, cpu_limit=excluded.cpu_limit, catalog=excluded.catalog,
			is_private=excluded.is_private, rate_limit=excluded.rate_limit`,
		s.App, s.Team, s.Repo, s.Branch, s.RootDir, s.Port, s.CloneURL, b2i(s.Auto), s.Parent, b2i(s.PreviewAuto), s.Replicas, s.Release,
		b2i(s.Autoscale), s.ScaleMin, s.ScaleMax, s.ScaleCPU, s.Origin, s.SleepAfter, s.HealthPath, s.HealthTimeout, s.Image, s.Memory, s.CPU, s.Catalog, b2i(s.Private), s.RateLimit)
	return err
}

func scanSource(row interface{ Scan(...any) error }) (Source, bool) {
	var s Source
	var auto, previewAuto, autoscale, private int
	if row.Scan(&s.App, &s.Team, &s.Repo, &s.Branch, &s.RootDir, &s.Port, &s.CloneURL, &auto, &s.Parent, &previewAuto, &s.Replicas, &s.Release,
		&autoscale, &s.ScaleMin, &s.ScaleMax, &s.ScaleCPU, &s.Origin, &s.SleepAfter, &s.HealthPath, &s.HealthTimeout, &s.Image, &s.Memory, &s.CPU, &s.Catalog, &private, &s.RateLimit) != nil {
		return Source{}, false
	}
	s.Auto = auto != 0
	s.PreviewAuto = previewAuto != 0
	s.Autoscale = autoscale != 0
	s.Private = private != 0
	if s.Replicas < 1 {
		s.Replicas = 1
	}
	return s, true
}

func productionAppsForRepo(repo string) []Source {
	rows, err := sqlDB.Query(`SELECT `+sourceCols+` FROM sources WHERE repo=? AND parent=''`, repo)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Source
	for rows.Next() {
		if s, ok := scanSource(rows); ok {
			out = append(out, s)
		}
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

func allSources() []Source {
	rows, err := sqlDB.Query(`SELECT ` + sourceCols + ` FROM sources`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Source
	for rows.Next() {
		if s, ok := scanSource(rows); ok {
			out = append(out, s)
		}
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

func getSource(app string) (Source, bool) {
	return scanSource(sqlDB.QueryRow(`SELECT `+sourceCols+` FROM sources WHERE app=?`, app))
}

// deleteSource cascades a project's rows in one transaction — the FK cascade we
// deliberately don't declare, so a mid-delete failure can't orphan half of them.
func deleteSource(app string) {
	tx, err := sqlDB.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback() //nolint:errcheck
	del := func(q string) bool { _, err := tx.Exec(q, app); return err == nil }
	if del(`DELETE FROM sources WHERE app=?`) &&
		del(`DELETE FROM deploys WHERE app=?`) &&
		del(`DELETE FROM env_vars WHERE app=?`) &&
		del(`DELETE FROM db_attachments WHERE app=?`) &&
		del(`DELETE FROM preview_passwords WHERE app=?`) &&
		del(`DELETE FROM preview_env WHERE app=?`) &&
		del(`DELETE FROM volumes WHERE app=?`) &&
		del(`DELETE FROM registry_creds WHERE app=?`) &&
		del(`DELETE FROM log_drain_cursors WHERE app=?`) {
		_ = tx.Commit()
	}
}

func sourcesForRepo(repo, branch string) []Source {
	rows, err := sqlDB.Query(`SELECT `+sourceCols+` FROM sources
		WHERE auto=1 AND repo=? AND (branch='' OR branch=?)`, repo, branch)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Source
	for rows.Next() {
		if s, ok := scanSource(rows); ok {
			out = append(out, s)
		}
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

func addDeploy(d Deploy) {
	_, _ = sqlDB.Exec(
		`INSERT INTO deploys(id,app,commit_sha,message,trigger,status,started) VALUES(?,?,?,?,?,?,?)`,
		d.ID, d.App, d.Commit, d.Message, d.Trigger, d.Status, d.Started)
}

func setDeployStatus(app, id, status string) {
	_, _ = sqlDB.Exec(`UPDATE deploys SET status=? WHERE id=? AND app=?`, status, id, app)
}

func appDeploys(app string) []Deploy { return appDeploysPage(app, 0, "", 20) }

func appDeploysPage(app string, before int64, beforeID string, limit int) []Deploy {
	q := `SELECT id,app,commit_sha,message,trigger,status,started FROM deploys WHERE app=?`
	args := []any{app}
	if before > 0 {
		q += ` AND (started < ? OR (started = ? AND id < ?))`
		args = append(args, before, before, beforeID)
	}
	q += ` ORDER BY started DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := sqlDB.Query(q, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []Deploy{}
	for rows.Next() {
		var d Deploy
		if rows.Scan(&d.ID, &d.App, &d.Commit, &d.Message, &d.Trigger, &d.Status, &d.Started) == nil {
			out = append(out, d)
		}
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

// teamDeploys is the global build feed for one team (plus the control plane),
// keyset-paginated on (started, id). Team-scoping the LIMIT in SQL keeps every
// page full no matter how the team's builds interleave with others'.
func teamDeploys(team string, before int64, beforeID string, limit int) []Deploy {
	q := `SELECT d.id, d.app, d.commit_sha, d.message, d.trigger, d.status, d.started
	      FROM deploys d
	      LEFT JOIN sources s ON s.app = d.app
	      WHERE (d.app = ? OR s.team = ?)`
	args := []any{controlPlaneApp, team}
	if before > 0 {
		q += ` AND (d.started < ? OR (d.started = ? AND d.id < ?))`
		args = append(args, before, before, beforeID)
	}
	q += ` ORDER BY d.started DESC, d.id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := sqlDB.Query(q, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []Deploy{}
	for rows.Next() {
		var d Deploy
		if rows.Scan(&d.ID, &d.App, &d.Commit, &d.Message, &d.Trigger, &d.Status, &d.Started) == nil {
			out = append(out, d)
		}
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

// reconcileStuckDeploys clears builds an earlier process left "building", but
// only past the cutoff so an in-flight blue-green build isn't disturbed.
func reconcileStuckDeploys() {
	cutoff := time.Now().Unix() - 15*60
	_, _ = sqlDB.Exec(`UPDATE deploys SET status='canceled' WHERE status='building' AND started < ?`, cutoff)
}

func deployStatus(app, id string) string {
	var st string
	_ = sqlDB.QueryRow(`SELECT status FROM deploys WHERE id=? AND app=?`, id, app).Scan(&st)
	return st
}

func getDeployByID(id string) (Deploy, bool) {
	var d Deploy
	err := sqlDB.QueryRow(
		`SELECT id,app,commit_sha,message,trigger,status,started FROM deploys WHERE id=?`, id).
		Scan(&d.ID, &d.App, &d.Commit, &d.Message, &d.Trigger, &d.Status, &d.Started)
	return d, err == nil
}

func getEnv(app string) []EnvVar { return readEnv(`env_vars`, app) }

func setEnv(app string, vars []EnvVar) error { return writeEnv(`env_vars`, app, vars) }

// getPreviewEnv and setPreviewEnv hold a project's overrides for its previews, e.g. a separate DATABASE_URL.
func getPreviewEnv(app string) []EnvVar { return readEnv(`preview_env`, app) }

func setPreviewEnv(app string, vars []EnvVar) error { return writeEnv(`preview_env`, app, vars) }

// dropCopiedPreviewEnv removes vars previews were given as a copy of their project's when they were created; they now read the project's live, and a stale copy would hide later changes and the preview overrides.
func dropCopiedPreviewEnv() {
	_, _ = sqlDB.Exec(`DELETE FROM env_vars WHERE app IN (SELECT app FROM sources WHERE parent<>'')
		AND EXISTS (SELECT 1 FROM sources s JOIN env_vars pe ON pe.app = s.parent
			WHERE s.app = env_vars.app AND pe.key = env_vars.key AND pe.value = env_vars.value AND pe.build = env_vars.build)`)
}

// readEnv and writeEnv take the table name from the constants above, never from input.
func readEnv(table, app string) []EnvVar {
	rows, err := sqlDB.Query(`SELECT key,value,build FROM `+table+` WHERE app=? ORDER BY key`, app)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []EnvVar{}
	for rows.Next() {
		var e EnvVar
		var build int
		if rows.Scan(&e.Key, &e.Value, &build) == nil {
			e.Build = build != 0
			out = append(out, e)
		}
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

func writeEnv(table, app string, vars []EnvVar) error {
	tx, err := sqlDB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(`DELETE FROM `+table+` WHERE app=?`, app); err != nil {
		return err
	}
	for _, e := range vars {
		key := sanitizeEnvKey(e.Key)
		if key == "" {
			continue
		}
		if _, err := tx.Exec(
			`INSERT INTO `+table+`(app,key,value,build) VALUES(?,?,?,?)`,
			app, key, e.Value, b2i(e.Build)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func sharedEnv(team string) []EnvVar {
	rows, err := sqlDB.Query(`SELECT key,value,build FROM shared_env WHERE team=? ORDER BY key`, team)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []EnvVar{}
	for rows.Next() {
		var e EnvVar
		var build int
		if rows.Scan(&e.Key, &e.Value, &build) == nil {
			e.Build = build != 0
			out = append(out, e)
		}
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

func setSharedEnv(team string, vars []EnvVar) error {
	tx, err := sqlDB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(`DELETE FROM shared_env WHERE team=?`, team); err != nil {
		return err
	}
	for _, e := range vars {
		key := sanitizeEnvKey(e.Key)
		if key == "" {
			continue
		}
		if _, err := tx.Exec(
			`INSERT INTO shared_env(team,key,value,build) VALUES(?,?,?,?)`,
			team, key, e.Value, b2i(e.Build)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// deployEnv merges the team's shared vars with the app's own; the app wins on a
// key conflict.
// deployEnv layers team-shared vars, then the project's own; a preview reads its project's vars fresh each deploy, then the preview overrides, then anything set on that preview alone.
func deployEnv(src Source) []EnvVar {
	layers := [][]EnvVar{sharedEnv(src.Team)}
	if src.Parent != "" {
		layers = append(layers, getEnv(src.Parent), getPreviewEnv(src.Parent))
	}
	layers = append(layers, getEnv(src.App))
	merged := map[string]EnvVar{}
	for _, layer := range layers {
		for _, e := range layer {
			merged[e.Key] = e
		}
	}
	out := make([]EnvVar, 0, len(merged))
	for _, e := range merged {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func sanitizeID(s string) string {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			out = append(out, byte(r))
		}
	}
	return string(out)
}

func sanitizeEnvKey(s string) string {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			out = append(out, byte(r))
		}
	}
	return string(out)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func putSession(token, userID, teamID string) error {
	_, err := sqlDB.Exec(
		`INSERT INTO sessions(token,user_id,team_id,created) VALUES(?,?,?,?)`,
		token, userID, teamID, time.Now().Unix())
	return err
}

const sessionMaxAge = 30 * 24 * 60 * 60 // 30 days

func getSession(token string) (sess, bool) {
	var s sess
	cutoff := time.Now().Unix() - sessionMaxAge
	if sqlDB.QueryRow(`SELECT user_id,team_id FROM sessions WHERE token=? AND created > ?`, token, cutoff).
		Scan(&s.userID, &s.teamID) != nil {
		return sess{}, false
	}
	return s, true
}

func deleteSession(token string) {
	_, _ = sqlDB.Exec(`DELETE FROM sessions WHERE token=?`, token)
}

func setSessionTeam(token, teamID string) {
	_, _ = sqlDB.Exec(`UPDATE sessions SET team_id=? WHERE token=?`, teamID, token)
}
