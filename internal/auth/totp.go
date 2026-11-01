package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	totpPeriod     = 30
	totpDigits     = 6
	recoveryCount  = 10
	recoveryLength = 10
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret makes a 160-bit secret, base32 as authenticator apps expect.
func NewTOTPSecret() string {
	b := make([]byte, 20)
	_, _ = rand.Read(b)
	return b32.EncodeToString(b)
}

// TOTPURI is what the enrolment QR code holds.
func TOTPURI(issuer, account, secret string) string {
	v := url.Values{}
	v.Set("secret", secret)
	v.Set("issuer", issuer)
	v.Set("period", fmt.Sprint(totpPeriod))
	v.Set("digits", fmt.Sprint(totpDigits))
	return "otpauth://totp/" + url.PathEscape(issuer+":"+account) + "?" + v.Encode()
}

func totpCode(secret string, step int64) (string, bool) {
	key, err := b32.DecodeString(strings.ToUpper(secret))
	if err != nil {
		return "", false
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	n := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, n%1_000_000), true
}

// matchTOTP finds the time step a code belongs to, allowing one step of clock drift either way; steps at or before after are refused so a code can't be replayed.
func matchTOTP(secret, code string, now time.Time, after int64) (int64, bool) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != totpDigits {
		return 0, false
	}
	cur := now.Unix() / totpPeriod
	for _, step := range []int64{cur - 1, cur, cur + 1} {
		if step <= after {
			continue
		}
		want, ok := totpCode(secret, step)
		if ok && subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}

// newRecoveryCodes makes single-use codes shaped like "abcde-fghij"; only their hashes are stored.
func newRecoveryCodes() []string {
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789"
	codes := make([]string, recoveryCount)
	for i := range codes {
		b := make([]byte, recoveryLength)
		_, _ = rand.Read(b)
		for j := range b {
			b[j] = alphabet[int(b[j])%len(alphabet)]
		}
		codes[i] = string(b[:recoveryLength/2]) + "-" + string(b[recoveryLength/2:])
	}
	return codes
}

func hashRecovery(code string) string {
	code = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
	code = strings.ReplaceAll(code, " ", "")
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

// CodeAt is the authenticator code for a secret at a moment.
func CodeAt(secret string, t time.Time) string {
	code, _ := totpCode(secret, t.Unix()/totpPeriod)
	return code
}
