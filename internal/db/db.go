// Package db opens Skiff's embedded SQLite database (pure-Go, no CGO) and applies the schema.
package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

func Open() (*sql.DB, error) {
	home, _ := os.UserHomeDir()
	return OpenAt(filepath.Join(home, ".skiff", "skiff.db"))
}

func OpenAt(path string) (*sql.DB, error) {
	// Holds session tokens plus password/API-token/DB-credential hashes, so lock the dir (0700) and file (0600) against other local users.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	_ = os.Chmod(dir, 0o700) // tighten a pre-existing dir created world-traversable
	if f, err := os.OpenFile(path, os.O_CREATE, 0o600); err == nil {
		_ = f.Close()
	}
	_ = os.Chmod(path, 0o600) // fix a pre-existing world-readable db file
	dsn := "file:" + path +
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)"
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite is single-writer; serialize to avoid lock churn on a low-traffic panel.
	d.SetMaxOpenConns(1)
	if _, err := d.Exec(schema); err != nil {
		return nil, err
	}
	// ADD COLUMN migrations re-run on every open; tolerate the "duplicate column name" failure but surface any other as a genuinely broken migration.
	for _, m := range migrations {
		if _, err := d.Exec(m); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			return nil, fmt.Errorf("migration %q: %w", m, err)
		}
	}
	return d, nil
}

// migrations are additive and safe to re-run; each fails harmlessly once applied.
var migrations = []string{
	`ALTER TABLE deploys ADD COLUMN message TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE sources ADD COLUMN parent TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE sources ADD COLUMN preview_auto INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE databases ADD COLUMN public INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE databases ADD COLUMN public_port INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE sources ADD COLUMN replicas INTEGER NOT NULL DEFAULT 1`,
	`ALTER TABLE sources ADD COLUMN release TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE sources ADD COLUMN autoscale INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE sources ADD COLUMN scale_min INTEGER NOT NULL DEFAULT 1`,
	`ALTER TABLE sources ADD COLUMN scale_max INTEGER NOT NULL DEFAULT 1`,
	`ALTER TABLE sources ADD COLUMN scale_cpu INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE domains ADD COLUMN parent TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE domains ADD COLUMN branch TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE sources ADD COLUMN origin TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE databases ADD COLUMN image TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE databases ADD COLUMN volume TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE databases ADD COLUMN cmd TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE backups ADD COLUMN offsite TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE sources ADD COLUMN stack TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE sources ADD COLUMN service TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE sources ADD COLUMN command TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE sources ADD COLUMN dockerfile TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE sources ADD COLUMN health_mode TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE databases ADD COLUMN stack TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE databases ADD COLUMN alias TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE stacks ADD COLUMN vars TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE sources ADD COLUMN preview_data TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE sources ADD COLUMN tcp_port INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE databases ADD COLUMN preview_app TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE stacks ADD COLUMN held TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE stacks ADD COLUMN parent TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE stacks ADD COLUMN preview_data TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE stacks ADD COLUMN preview_password TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE stacks ADD COLUMN preview_error TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE sources ADD COLUMN sleep_after INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE sources ADD COLUMN health_path TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE sources ADD COLUMN health_timeout INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE users ADD COLUMN totp_secret TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE users ADD COLUMN totp_pending TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE users ADD COLUMN totp_step INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE sources ADD COLUMN image TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE sources ADD COLUMN memory_limit TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE sources ADD COLUMN cpu_limit TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE sources ADD COLUMN catalog TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE sources ADD COLUMN is_private INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE sources ADD COLUMN rate_limit INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE backups ADD COLUMN check_status TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE backups ADD COLUMN check_detail TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE backups ADD COLUMN check_items INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE backups ADD COLUMN check_at INTEGER NOT NULL DEFAULT 0`,
}

const schema = `
CREATE TABLE IF NOT EXISTS users (
  id            TEXT PRIMARY KEY,
  email         TEXT UNIQUE NOT NULL,
  name          TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL,
  created       INTEGER NOT NULL,
  totp_secret   TEXT NOT NULL DEFAULT '',
  totp_pending  TEXT NOT NULL DEFAULT '',
  totp_step     INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS recovery_codes (
  user_id TEXT NOT NULL,
  hash    TEXT NOT NULL,
  PRIMARY KEY (user_id, hash)
);
CREATE TABLE IF NOT EXISTS teams (
  id      TEXT PRIMARY KEY,
  name    TEXT NOT NULL,
  slug    TEXT NOT NULL,
  created INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS memberships (
  user_id TEXT NOT NULL,
  team_id TEXT NOT NULL,
  role    TEXT NOT NULL,
  PRIMARY KEY (user_id, team_id)
);
CREATE TABLE IF NOT EXISTS invites (
  token   TEXT PRIMARY KEY,
  email   TEXT NOT NULL,
  team_id TEXT NOT NULL,
  role    TEXT NOT NULL,
  created INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sources (
  app       TEXT PRIMARY KEY,
  team      TEXT NOT NULL DEFAULT '',
  repo      TEXT NOT NULL DEFAULT '',
  branch    TEXT NOT NULL DEFAULT '',
  root_dir  TEXT NOT NULL DEFAULT '',
  port      TEXT NOT NULL DEFAULT '3000',
  clone_url    TEXT NOT NULL DEFAULT '',
  auto         INTEGER NOT NULL DEFAULT 0,
  parent       TEXT NOT NULL DEFAULT '',
  preview_auto INTEGER NOT NULL DEFAULT 0,
  replicas     INTEGER NOT NULL DEFAULT 1,
  release      TEXT NOT NULL DEFAULT '',
  autoscale    INTEGER NOT NULL DEFAULT 0,
  scale_min    INTEGER NOT NULL DEFAULT 1,
  scale_max    INTEGER NOT NULL DEFAULT 1,
  scale_cpu    INTEGER NOT NULL DEFAULT 0,
  origin       TEXT NOT NULL DEFAULT '',
  sleep_after  INTEGER NOT NULL DEFAULT 0,
  health_path  TEXT NOT NULL DEFAULT '',
  health_timeout INTEGER NOT NULL DEFAULT 0,
  image        TEXT NOT NULL DEFAULT '',
  memory_limit TEXT NOT NULL DEFAULT '',
  cpu_limit    TEXT NOT NULL DEFAULT '',
  catalog      TEXT NOT NULL DEFAULT '',
  is_private   INTEGER NOT NULL DEFAULT 0,
  rate_limit   INTEGER NOT NULL DEFAULT 0,
  stack        TEXT NOT NULL DEFAULT '',
  service      TEXT NOT NULL DEFAULT '',
  command      TEXT NOT NULL DEFAULT '',
  dockerfile   TEXT NOT NULL DEFAULT '',
  health_mode  TEXT NOT NULL DEFAULT '',
  preview_data TEXT NOT NULL DEFAULT '',
  tcp_port     INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS deploys (
  id         TEXT PRIMARY KEY,
  app        TEXT NOT NULL,
  commit_sha TEXT NOT NULL DEFAULT '',
  message    TEXT NOT NULL DEFAULT '',
  trigger    TEXT NOT NULL DEFAULT 'manual',
  status     TEXT NOT NULL DEFAULT 'building',
  started    INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_deploys_app ON deploys(app, started DESC);
CREATE INDEX IF NOT EXISTS idx_deploys_started ON deploys(started DESC, id DESC);
CREATE TABLE IF NOT EXISTS env_vars (
  app   TEXT NOT NULL,
  key   TEXT NOT NULL,
  value TEXT NOT NULL DEFAULT '',
  build INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (app, key)
);
CREATE TABLE IF NOT EXISTS shared_env (
  team  TEXT NOT NULL,
  key   TEXT NOT NULL,
  value TEXT NOT NULL DEFAULT '',
  build INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (team, key)
);
CREATE TABLE IF NOT EXISTS domains (
  host    TEXT PRIMARY KEY,
  app     TEXT NOT NULL,
  team    TEXT NOT NULL DEFAULT '',
  parent  TEXT NOT NULL DEFAULT '',
  branch  TEXT NOT NULL DEFAULT '',
  created INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
  token   TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  team_id TEXT NOT NULL DEFAULT '',
  created INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS databases (
  id        TEXT PRIMARY KEY,
  team      TEXT NOT NULL,
  name      TEXT NOT NULL,
  engine    TEXT NOT NULL,
  container TEXT NOT NULL,
  host      TEXT NOT NULL,
  port      INTEGER NOT NULL,
  username  TEXT NOT NULL DEFAULT '',
  password  TEXT NOT NULL DEFAULT '',
  dbname    TEXT NOT NULL DEFAULT '',
  created   INTEGER NOT NULL,
  public      INTEGER NOT NULL DEFAULT 0,
  public_port INTEGER NOT NULL DEFAULT 0,
  image       TEXT NOT NULL DEFAULT '',
  volume      TEXT NOT NULL DEFAULT '',
  cmd         TEXT NOT NULL DEFAULT '',
  stack       TEXT NOT NULL DEFAULT '',
  alias       TEXT NOT NULL DEFAULT '',
  preview_app TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS db_attachments (
  db_id TEXT NOT NULL,
  app   TEXT NOT NULL,
  var   TEXT NOT NULL,
  PRIMARY KEY (db_id, app)
);
CREATE TABLE IF NOT EXISTS backups (
  id      TEXT PRIMARY KEY,
  db_id   TEXT NOT NULL,
  team    TEXT NOT NULL,
  engine  TEXT NOT NULL,
  file    TEXT NOT NULL,
  size    INTEGER NOT NULL DEFAULT 0,
  trigger TEXT NOT NULL DEFAULT 'manual',
  created INTEGER NOT NULL,
  offsite TEXT NOT NULL DEFAULT '',
  check_status TEXT NOT NULL DEFAULT '',
  check_detail TEXT NOT NULL DEFAULT '',
  check_items  INTEGER NOT NULL DEFAULT 0,
  check_at     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_backups_db ON backups(db_id, created DESC);
CREATE TABLE IF NOT EXISTS jobs (
  id       TEXT PRIMARY KEY,
  app      TEXT NOT NULL,
  team     TEXT NOT NULL,
  name     TEXT NOT NULL,
  schedule TEXT NOT NULL,
  command  TEXT NOT NULL,
  last_run INTEGER NOT NULL DEFAULT 0,
  last_ok  INTEGER NOT NULL DEFAULT 1,
  created  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_jobs_app ON jobs(app);
CREATE TABLE IF NOT EXISTS alerts (
  team        TEXT PRIMARY KEY,
  email       TEXT NOT NULL DEFAULT '',
  slack_url   TEXT NOT NULL DEFAULT '',
  webhook_url TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS audit (
  id      INTEGER PRIMARY KEY AUTOINCREMENT,
  team    TEXT NOT NULL,
  actor   TEXT NOT NULL,
  action  TEXT NOT NULL,
  target  TEXT NOT NULL DEFAULT '',
  detail  TEXT NOT NULL DEFAULT '',
  created INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_team ON audit(team, id DESC);
CREATE TABLE IF NOT EXISTS api_tokens (
  id         TEXT PRIMARY KEY,
  team       TEXT NOT NULL,
  name       TEXT NOT NULL,
  token_hash TEXT NOT NULL UNIQUE,
  created    INTEGER NOT NULL,
  last_used  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_api_tokens_team ON api_tokens(team, created DESC);
CREATE TABLE IF NOT EXISTS buckets (
  id         TEXT PRIMARY KEY,
  team       TEXT NOT NULL,
  name       TEXT NOT NULL,
  container  TEXT NOT NULL,
  access_key TEXT NOT NULL,
  secret_key TEXT NOT NULL,
  created    INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS bucket_attachments (
  bucket_id TEXT NOT NULL,
  app       TEXT NOT NULL,
  PRIMARY KEY (bucket_id, app)
);
CREATE TABLE IF NOT EXISTS workers (
  id       TEXT PRIMARY KEY,
  app      TEXT NOT NULL,
  team     TEXT NOT NULL,
  name     TEXT NOT NULL,
  command  TEXT NOT NULL,
  replicas INTEGER NOT NULL DEFAULT 1,
  created  INTEGER NOT NULL,
  UNIQUE (app, name)
);
CREATE INDEX IF NOT EXISTS idx_workers_app ON workers(app);
CREATE TABLE IF NOT EXISTS mail_forwards (
  id           TEXT PRIMARY KEY,
  team         TEXT NOT NULL,
  domain       TEXT NOT NULL,
  alias        TEXT NOT NULL,
  destinations TEXT NOT NULL,
  created      INTEGER NOT NULL,
  UNIQUE (domain, alias)
);
CREATE INDEX IF NOT EXISTS idx_mail_forwards_team ON mail_forwards(team);
CREATE TABLE IF NOT EXISTS backup_targets (
  team       TEXT PRIMARY KEY,
  endpoint   TEXT NOT NULL,
  region     TEXT NOT NULL DEFAULT '',
  bucket     TEXT NOT NULL,
  prefix     TEXT NOT NULL DEFAULT '',
  access_key TEXT NOT NULL,
  secret_key TEXT NOT NULL,
  updated    INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS requests (
  t       INTEGER NOT NULL,
  app     TEXT NOT NULL,
  method  TEXT NOT NULL DEFAULT '',
  path    TEXT NOT NULL DEFAULT '',
  status  INTEGER NOT NULL DEFAULT 0,
  dur     INTEGER NOT NULL DEFAULT 0,
  bytes   INTEGER NOT NULL DEFAULT 0,
  ref     TEXT NOT NULL DEFAULT '',
  visitor TEXT NOT NULL DEFAULT '',
  bot     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_requests_app_t ON requests(app, t);
CREATE INDEX IF NOT EXISTS idx_requests_t ON requests(t);
CREATE TABLE IF NOT EXISTS request_ingest (
  file   TEXT PRIMARY KEY,
  pos    INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS instance_settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS disk_samples (
  t     INTEGER NOT NULL,
  used  INTEGER NOT NULL,
  total INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_disk_samples_t ON disk_samples(t);
CREATE TABLE IF NOT EXISTS uptime_checks (
  app  TEXT NOT NULL,
  t    INTEGER NOT NULL,
  ok   INTEGER NOT NULL,
  code INTEGER NOT NULL DEFAULT 0,
  ms   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_uptime_app_t ON uptime_checks(app, t);
CREATE TABLE IF NOT EXISTS incidents (
  id      INTEGER PRIMARY KEY AUTOINCREMENT,
  app     TEXT NOT NULL,
  team    TEXT NOT NULL DEFAULT '',
  started INTEGER NOT NULL,
  ended   INTEGER NOT NULL DEFAULT 0,
  cause   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_incidents_app ON incidents(app, started DESC);
CREATE TABLE IF NOT EXISTS pr_comments (
  app        TEXT NOT NULL,
  pr         INTEGER NOT NULL,
  comment_id INTEGER NOT NULL,
  PRIMARY KEY (app, pr)
);
CREATE TABLE IF NOT EXISTS cp_backups (
  key     TEXT PRIMARY KEY,
  size    INTEGER NOT NULL,
  created INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS preview_env (
  app   TEXT NOT NULL,
  key   TEXT NOT NULL,
  value TEXT NOT NULL DEFAULT '',
  build INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (app, key)
);
CREATE TABLE IF NOT EXISTS volumes (
  app     TEXT NOT NULL,
  name    TEXT NOT NULL,
  path    TEXT NOT NULL,
  created INTEGER NOT NULL,
  PRIMARY KEY (app, name)
);
CREATE TABLE IF NOT EXISTS stacks (
  name      TEXT PRIMARY KEY,
  team      TEXT NOT NULL,
  repo      TEXT NOT NULL DEFAULT '',
  branch    TEXT NOT NULL DEFAULT '',
  path      TEXT NOT NULL DEFAULT '',
  clone_url TEXT NOT NULL DEFAULT '',
  created   INTEGER NOT NULL,
  vars      TEXT NOT NULL DEFAULT '',
  held      TEXT NOT NULL DEFAULT '',
  parent           TEXT NOT NULL DEFAULT '',
  preview_data     TEXT NOT NULL DEFAULT '',
  preview_password TEXT NOT NULL DEFAULT '',
  preview_error    TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS volume_backups (
  id          TEXT PRIMARY KEY,
  app         TEXT NOT NULL,
  volume      TEXT NOT NULL,
  team        TEXT NOT NULL,
  file        TEXT NOT NULL,
  size        INTEGER NOT NULL DEFAULT 0,
  trigger     TEXT NOT NULL DEFAULT 'manual',
  created     INTEGER NOT NULL,
  offsite     TEXT NOT NULL DEFAULT '',
  offsite_key TEXT NOT NULL DEFAULT '',
  live        INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_volume_backups ON volume_backups(app, volume, created DESC);
CREATE TABLE IF NOT EXISTS log_drains (
  team         TEXT PRIMARY KEY,
  url          TEXT NOT NULL,
  header_name  TEXT NOT NULL DEFAULT '',
  header_value TEXT NOT NULL DEFAULT '',
  last_ok      INTEGER NOT NULL DEFAULT 0,
  last_error   TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS log_drain_cursors (
  team TEXT NOT NULL,
  app  TEXT NOT NULL,
  day  TEXT NOT NULL,
  pos  INTEGER NOT NULL,
  PRIMARY KEY (team, app)
);
CREATE TABLE IF NOT EXISTS status_pages (
  team    TEXT PRIMARY KEY,
  slug    TEXT NOT NULL UNIQUE,
  enabled INTEGER NOT NULL DEFAULT 0,
  title   TEXT NOT NULL DEFAULT '',
  host    TEXT NOT NULL DEFAULT '',
  apps    TEXT NOT NULL DEFAULT '[]'
);
CREATE TABLE IF NOT EXISTS registry_creds (
  app      TEXT PRIMARY KEY,
  username TEXT NOT NULL,
  password TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS preview_passwords (
  app  TEXT PRIMARY KEY,
  hash TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS offsite_objects (
  db_id   TEXT NOT NULL,
  key     TEXT NOT NULL,
  created INTEGER NOT NULL,
  PRIMARY KEY (db_id, key)
);
`
