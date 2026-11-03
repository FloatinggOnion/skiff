package panel

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/nuelScript/skiff/internal/auth"
	"github.com/nuelScript/skiff/internal/seal"
)

const (
	sealCanaryKey  = "seal_canary"
	sealCanaryText = "skiff"
)

// initSeal loads the field key and proves it matches this database. Refusing to start beats running with secrets it can't read, or replacing a lost key with a new one.
func initSeal() error {
	var canary string
	err := sqlDB.QueryRow(`SELECT value FROM instance_settings WHERE key=?`, sealCanaryKey).Scan(&canary)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	path := filepath.Join(skiffDir(), "recovery.key")
	if canary != "" {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("this database's secrets need %s, which can't be read (%w). Put it back: the key is 64 hex characters, shown under Server → Control-plane backup when it was saved", path, err)
		}
	}
	k, err := recoveryKey()
	if err != nil {
		return fmt.Errorf("loading %s: %w", path, err)
	}
	if err := seal.Init(k); err != nil {
		return err
	}
	if canary == "" {
		s, err := seal.Seal(sealCanaryText)
		if err != nil {
			return err
		}
		_, err = sqlDB.Exec(`INSERT OR IGNORE INTO instance_settings(key,value) VALUES(?,?)`, sealCanaryKey, s)
		return err
	}
	if got, err := seal.Open(canary); err != nil || got != sealCanaryText {
		return fmt.Errorf("%s doesn't match this database, so its secrets can't be read. Restore the key this database was created with", path)
	}
	return nil
}

// sealedColumns lists every stored secret that is encrypted at rest.
var sealedColumns = []struct {
	table string
	cols  []string
}{
	{"env_vars", []string{"value"}},
	{"preview_env", []string{"value"}},
	{"shared_env", []string{"value"}},
	{"databases", []string{"password"}},
	{"alerts", []string{"slack_url", "webhook_url"}},
	{"log_drains", []string{"url", "header_value"}},
	{"registry_creds", []string{"password"}},
	{"backup_targets", []string{"access_key", "secret_key"}},
	{"buckets", []string{"access_key", "secret_key"}},
	{"users", []string{"totp_secret", "totp_pending"}},
}

const (
	sealScrubKey      = "seal_scrub_pending"
	preEncryptionKeep = 14 * 24 * time.Hour
	preEncryptionGlob = "pre-encryption-*.skiffbackup"
	preEncryptionFile = "pre-encryption-%d.skiffbackup"
)

type plainSecret struct {
	table, col string
	rowid      int64
	plain      string
}

func findPlainSecrets() ([]plainSecret, error) {
	var out []plainSecret
	for _, t := range sealedColumns {
		for _, col := range t.cols {
			rows, err := sqlDB.Query(`SELECT rowid, ` + col + ` FROM ` + t.table + ` WHERE ` + col + ` <> ''`)
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				p := plainSecret{table: t.table, col: col}
				if err := rows.Scan(&p.rowid, &p.plain); err != nil {
					rows.Close()
					return nil, err
				}
				if !seal.IsSealed(p.plain) {
					out = append(out, p)
				}
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// sealExisting encrypts secrets stored before encryption, or by an older panel still running during a self-update. It runs before the panel serves anything.
func sealExisting() error {
	plain, err := findPlainSecrets()
	if err != nil {
		return err
	}
	if len(plain) > 0 {
		bundle, err := controlPlaneBundle()
		if err != nil {
			return fmt.Errorf("backing up first: %w", err)
		}
		name := filepath.Join(skiffDir(), fmt.Sprintf(preEncryptionFile, time.Now().Unix()))
		if err := os.WriteFile(name, bundle, 0o600); err != nil {
			return fmt.Errorf("backing up first: %w", err)
		}
		if err := sealRows(plain); err != nil {
			return err
		}
		log.Printf("skiff: encrypted %d stored secrets; the previous database is sealed in %s", len(plain), name)
	}
	if err := scrubOldPages(); err != nil {
		return err
	}
	removeOldPreEncryptionBackups(time.Now())
	return nil
}

func sealRows(plain []plainSecret) error {
	tx, err := sqlDB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	for _, p := range plain {
		s, err := seal.Seal(p.plain)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE `+p.table+` SET `+p.col+`=? WHERE rowid=? AND `+p.col+`=?`, s, p.rowid, p.plain); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO instance_settings(key,value) VALUES(?,'1')`, sealScrubKey); err != nil {
		return err
	}
	return tx.Commit()
}

// scrubOldPages rewrites the database so the replaced plain values don't linger in free pages or the write-ahead log. The flag makes an interrupted scrub run again on the next start.
func scrubOldPages() error {
	var pending string
	err := sqlDB.QueryRow(`SELECT value FROM instance_settings WHERE key=?`, sealScrubKey).Scan(&pending)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := sqlDB.Exec(`VACUUM`); err != nil {
		return fmt.Errorf("clearing old values: %w", err)
	}
	if _, err := sqlDB.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return fmt.Errorf("clearing old values: %w", err)
	}
	_, err = sqlDB.Exec(`DELETE FROM instance_settings WHERE key=?`, sealScrubKey)
	return err
}

func removeOldPreEncryptionBackups(now time.Time) {
	files, _ := filepath.Glob(filepath.Join(skiffDir(), preEncryptionGlob))
	for _, f := range files {
		if info, err := os.Stat(f); err == nil && now.Sub(info.ModTime()) > preEncryptionKeep {
			_ = os.Remove(f)
		}
	}
}

// hashStoredTokens replaces session and invite tokens stored in full, so people stay signed in and open invites keep working. sealExisting's scrub then clears the old values.
func hashStoredTokens() error {
	type row struct{ table, token string }
	var found []row
	for _, table := range []string{"sessions", "invites"} {
		rows, err := sqlDB.Query(`SELECT token FROM ` + table)
		if err != nil {
			return err
		}
		for rows.Next() {
			var tok string
			if err := rows.Scan(&tok); err != nil {
				rows.Close()
				return err
			}
			if !auth.IsStoredToken(tok) {
				found = append(found, row{table, tok})
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	if len(found) == 0 {
		return nil
	}
	tx, err := sqlDB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	for _, r := range found {
		if _, err := tx.Exec(`UPDATE `+r.table+` SET token=? WHERE token=?`, auth.StoredToken(r.token), r.token); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO instance_settings(key,value) VALUES(?,'1')`, sealScrubKey); err != nil {
		return err
	}
	return tx.Commit()
}
