// Package gate is the list of previews behind a password: the panel writes it, the router asks visitors for the password before letting them through.
package gate

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const CookieName = "skiff_preview"

// Apps maps each protected app to the bcrypt hash of its password.
type Apps map[string]string

func DefaultFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".skiff", "protected.json")
}

func Load(path string) Apps {
	out := Apps{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

func Save(path string, apps Apps) error {
	b, err := json.Marshal(apps)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func Hash(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

func Check(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// Cookie is keyed by the password's hash, so changing the password signs everyone out.
func Cookie(hash, app string, expires time.Time) string {
	exp := strconv.FormatInt(expires.Unix(), 10)
	return exp + "." + sign(hash, app, exp)
}

func ValidCookie(hash, app, value string, now time.Time) bool {
	exp, sig, ok := strings.Cut(value, ".")
	if !ok {
		return false
	}
	n, err := strconv.ParseInt(exp, 10, 64)
	if err != nil || now.Unix() > n {
		return false
	}
	return hmac.Equal([]byte(sig), []byte(sign(hash, app, exp)))
}

func sign(hash, app, exp string) string {
	m := hmac.New(sha256.New, []byte(hash))
	fmt.Fprintf(m, "%s|%s", app, exp)
	return hex.EncodeToString(m.Sum(nil))
}
