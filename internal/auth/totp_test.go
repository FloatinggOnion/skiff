package auth

import (
	"encoding/base32"
	"strings"
	"testing"
	"time"

	"github.com/nuelScript/skiff/internal/seal"
)

// RFC 6238 appendix B, SHA-1, truncated to six digits.
func TestTOTPVectors(t *testing.T) {
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	for _, tc := range []struct {
		unix int64
		code string
	}{{59, "287082"}, {1111111109, "081804"}, {1111111111, "050471"}, {1234567890, "005924"}, {2000000000, "279037"}} {
		got, ok := totpCode(secret, tc.unix/totpPeriod)
		if !ok || got != tc.code {
			t.Errorf("at %d: got %q, want %q", tc.unix, got, tc.code)
		}
	}
}

func TestMatchTOTP(t *testing.T) {
	secret := NewTOTPSecret()
	now := time.Unix(1_800_000_000, 0)
	step := now.Unix() / totpPeriod
	code, _ := totpCode(secret, step)
	if got, ok := matchTOTP(secret, code[:3]+" "+code[3:], now, 0); !ok || got != step {
		t.Fatalf("current code refused (step %d, ok %v)", got, ok)
	}
	if _, ok := matchTOTP(secret, code, now, step); ok {
		t.Fatal("a used code was accepted again")
	}
	prev, _ := totpCode(secret, step-1)
	if _, ok := matchTOTP(secret, prev, now, 0); !ok {
		t.Fatal("the previous step should be allowed for clock drift")
	}
	old, _ := totpCode(secret, step-2)
	if _, ok := matchTOTP(secret, old, now, 0); ok {
		t.Fatal("a code from a minute ago was accepted")
	}
	if _, ok := matchTOTP(secret, "12345", now, 0); ok {
		t.Fatal("a short code was accepted")
	}
}

func TestRecoveryCodes(t *testing.T) {
	codes := newRecoveryCodes()
	seen := map[string]bool{}
	for _, c := range codes {
		if len(c) != recoveryLength+1 || c[recoveryLength/2] != '-' || seen[c] {
			t.Fatalf("bad code %q", c)
		}
		seen[c] = true
	}
	if hashRecovery(codes[0]) != hashRecovery(" "+strings.ToUpper(strings.ReplaceAll(codes[0], "-", ""))+" ") {
		t.Fatal("a code should match however it's typed")
	}
	if !strings.HasPrefix(TOTPURI("Skiff", "a@b.c", "ABC"), "otpauth://totp/Skiff:a@b.c?") {
		t.Fatal(TOTPURI("Skiff", "a@b.c", "ABC"))
	}
}

func TestSecondFactorReadsSealedSecret(t *testing.T) {
	s := newTestStore(t)
	u, _, err := s.CreateUser("sealed@example.com", "Sealed", "password123")
	if err != nil {
		t.Fatal(err)
	}
	const secret = "JBSWY3DPEHPK3PXP"
	sealed, err := seal.Seal(secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE users SET totp_secret = ? WHERE id = ?`, sealed, u.ID); err != nil {
		t.Fatal(err)
	}
	if !s.CheckSecondFactor(u.ID, CodeAt(secret, time.Now())) {
		t.Fatal("a current code should pass against a sealed secret")
	}
}
