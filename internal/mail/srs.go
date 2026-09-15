package mail

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	srsAlphabet = "abcdefghijklmnopqrstuvwxyz234567"
	srsMaxAge   = 21 // days a bounce address stays valid
)

var srsEncoding = base32.NewEncoding(srsAlphabet).WithPadding(base32.NoPadding)

// SRS rewrites a forwarded message's sender onto the forwarding domain so SPF keeps passing, while still routing bounces home.
type SRS struct{ Key []byte }

// LoadKey reads the SRS secret at path, creating one on first use.
func LoadKey(path string) ([]byte, error) {
	if b, err := os.ReadFile(path); err == nil {
		if k, err := hex.DecodeString(strings.TrimSpace(string(b))); err == nil && len(k) >= 16 {
			return k, nil
		}
		return nil, errors.New("mail: malformed SRS key in " + path)
	}
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	// Link in the finished file, so two processes starting together can't each mint a key and invalidate the other's bounce addresses.
	tmp := path + ".new"
	if err := os.WriteFile(tmp, []byte(hex.EncodeToString(k)), 0o600); err != nil {
		return nil, err
	}
	defer os.Remove(tmp)
	if err := os.Link(tmp, path); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return LoadKey(path)
		}
		return nil, err
	}
	return k, nil
}

func srsDay(t time.Time) int { return int(t.Unix()/86400) % 1024 }

func srsStamp(t time.Time) string {
	d := srsDay(t)
	return string([]byte{srsAlphabet[d>>5], srsAlphabet[d&31]})
}

func (s SRS) hash(stamp, host, local string) string {
	m := hmac.New(sha256.New, s.Key)
	for _, p := range []string{stamp, host, local} {
		m.Write([]byte(strings.ToLower(p)))
		m.Write([]byte{0})
	}
	return srsEncoding.EncodeToString(m.Sum(nil))[:6]
}

func (s SRS) Forward(sender, domain string, now time.Time) string {
	local, host, ok := splitAddr(sender)
	if !ok || host == strings.ToLower(domain) {
		return sender
	}
	stamp := srsStamp(now)
	return "SRS0=" + s.hash(stamp, host, local) + "=" + stamp + "=" + host + "=" + local + "@" + domain
}

// IsSRS reports whether addr's local part is shaped like a rewritten sender, valid or not.
func IsSRS(addr string) bool {
	local, _, ok := splitAddr(addr)
	return ok && len(local) > 5 && strings.EqualFold(local[:5], "SRS0=")
}

// Reverse decodes an address minted by Forward, rejecting forged or expired ones so it can't be used to relay.
func (s SRS) Reverse(addr string, now time.Time) (string, bool) {
	if !IsSRS(addr) {
		return "", false
	}
	local, _, _ := splitAddr(addr)
	parts := strings.SplitN(local[5:], "=", 4)
	if len(parts) != 4 || parts[2] == "" || parts[3] == "" {
		return "", false
	}
	sum, stamp, host, orig := strings.ToLower(parts[0]), strings.ToLower(parts[1]), parts[2], parts[3]
	if len(stamp) != 2 || !hmac.Equal([]byte(sum), []byte(s.hash(stamp, host, orig))) {
		return "", false
	}
	d := strings.IndexByte(srsAlphabet, stamp[0])<<5 | strings.IndexByte(srsAlphabet, stamp[1])
	if (srsDay(now)-d+1024)%1024 > srsMaxAge {
		return "", false
	}
	return orig + "@" + host, true
}
