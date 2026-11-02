package panel

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

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
