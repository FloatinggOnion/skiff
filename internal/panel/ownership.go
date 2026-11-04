package panel

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

const verifyLabel = "_skiff-verify."

// lookupTXT is a variable so tests can stand in for DNS.
var lookupTXT = func(ctx context.Context, name string) ([]string, error) {
	return net.DefaultResolver.LookupTXT(ctx, name)
}

// verifyValue is what a team publishes to prove it controls a host; it is bound to the team so it can't be copied.
func verifyValue(team, host string) (string, error) {
	key, err := recoveryKey()
	if err != nil {
		return "", err
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte("domain-verify\x00" + team + "\x00" + host))
	return "skiff-verify=" + hex.EncodeToString(m.Sum(nil)[:16]), nil
}

// exemptFromProof is true for whoever runs the server, who can already point any name at it.
func (p *Panel) exemptFromProof(r *http.Request) bool {
	team := p.teamID(r)
	if f := foundingTeam(); f != "" && f == team {
		return true
	}
	return p.isInstanceAdmin(r)
}

// proveOwnership returns "" when the team has published the TXT record for host, and otherwise a message saying what to publish.
func (p *Panel) proveOwnership(r *http.Request, host string) string {
	if p.exemptFromProof(r) {
		return ""
	}
	want, err := verifyValue(p.teamID(r), host)
	if err != nil {
		return "could not check domain ownership right now"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	txts, _ := lookupTXT(ctx, verifyLabel+host)
	for _, t := range txts {
		if strings.TrimSpace(t) == want {
			return ""
		}
	}
	return fmt.Sprintf("prove you own %s: add a TXT record named %s%s with the value %s, wait for DNS to update, then try again", host, verifyLabel, host, want)
}
