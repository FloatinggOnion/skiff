package auth

import (
	"database/sql"
	"errors"
	"time"

	"github.com/nuelScript/skiff/internal/seal"
)

var ErrBadCode = errors.New("that code didn't match — try the newest one from your app")

// HasTwoFactor reports whether signing in also needs a code.
func (s *Store) HasTwoFactor(userID string) bool {
	var secret string
	_ = s.db.QueryRow(`SELECT totp_secret FROM users WHERE id = ?`, userID).Scan(&secret)
	return secret != ""
}

// RecoveryLeft counts the unused recovery codes.
func (s *Store) RecoveryLeft(userID string) int {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM recovery_codes WHERE user_id = ?`, userID).Scan(&n)
	return n
}

// BeginTwoFactor stores a fresh secret that only takes effect once a code from it is confirmed.
func (s *Store) BeginTwoFactor(userID string) (string, error) {
	secret := NewTOTPSecret()
	sealed, err := seal.Seal(secret)
	if err != nil {
		return "", err
	}
	_, err = s.db.Exec(`UPDATE users SET totp_pending = ? WHERE id = ?`, sealed, userID)
	return secret, err
}

// EnableTwoFactor confirms the pending secret with a code from it and returns new recovery codes.
func (s *Store) EnableTwoFactor(userID, code string) ([]string, error) {
	var pending string
	if err := s.db.QueryRow(`SELECT totp_pending FROM users WHERE id = ?`, userID).Scan(&pending); err != nil || pending == "" || seal.OpenAll(&pending) != nil {
		return nil, errors.New("start two-factor setup again")
	}
	step, ok := matchTOTP(pending, code, time.Now(), 0)
	if !ok {
		return nil, ErrBadCode
	}
	sealed, err := seal.Seal(pending)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(`UPDATE users SET totp_secret = ?, totp_pending = '', totp_step = ? WHERE id = ?`, sealed, step, userID); err != nil {
		return nil, err
	}
	codes, err := replaceRecovery(tx, userID)
	if err != nil {
		return nil, err
	}
	return codes, tx.Commit()
}

// DisableTwoFactor turns the second step off and drops the recovery codes.
func (s *Store) DisableTwoFactor(userID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(`UPDATE users SET totp_secret = '', totp_pending = '', totp_step = 0 WHERE id = ?`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM recovery_codes WHERE user_id = ?`, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// RegenerateRecovery replaces every recovery code.
func (s *Store) RegenerateRecovery(userID string) ([]string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck
	codes, err := replaceRecovery(tx, userID)
	if err != nil {
		return nil, err
	}
	return codes, tx.Commit()
}

func replaceRecovery(tx *sql.Tx, userID string) ([]string, error) {
	if _, err := tx.Exec(`DELETE FROM recovery_codes WHERE user_id = ?`, userID); err != nil {
		return nil, err
	}
	codes := newRecoveryCodes()
	for _, c := range codes {
		if _, err := tx.Exec(`INSERT INTO recovery_codes (user_id, hash) VALUES (?, ?)`, userID, hashRecovery(c)); err != nil {
			return nil, err
		}
	}
	return codes, nil
}

// CheckSecondFactor accepts a current authenticator code, each at most once, or an unused recovery code, which is then spent.
func (s *Store) CheckSecondFactor(userID, code string) bool {
	var secret string
	var last int64
	if s.db.QueryRow(`SELECT totp_secret, totp_step FROM users WHERE id = ?`, userID).Scan(&secret, &last) != nil || secret == "" || seal.OpenAll(&secret) != nil {
		return false
	}
	if step, ok := matchTOTP(secret, code, time.Now(), last); ok {
		// Conditional on the step still being older, so two requests racing with one code can't both pass.
		res, err := s.db.Exec(`UPDATE users SET totp_step = ? WHERE id = ? AND totp_step < ?`, step, userID, step)
		if err != nil {
			return false
		}
		n, _ := res.RowsAffected()
		return n == 1
	}
	res, err := s.db.Exec(`DELETE FROM recovery_codes WHERE user_id = ? AND hash = ?`, userID, hashRecovery(code))
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n == 1
}
