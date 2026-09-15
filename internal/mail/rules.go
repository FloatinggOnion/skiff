// Package mail receives SMTP for forwarded addresses and relays each message on to its destinations.
package mail

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"strings"
)

// Rules maps a domain to its aliases; the "*" alias catches every other address on that domain.
type Rules map[string]map[string][]string

// LoadRules reads the forwarding table; a missing file means nothing is forwarded, while an unreadable one is an error so callers keep their last good copy.
func LoadRules(path string) (Rules, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Rules{}, nil
	}
	if err != nil {
		return nil, err
	}
	r := Rules{}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	return r, nil
}

func splitAddr(addr string) (local, domain string, ok bool) {
	i := strings.LastIndexByte(addr, '@')
	if i <= 0 || i == len(addr)-1 {
		return "", "", false
	}
	return addr[:i], strings.ToLower(addr[i+1:]), true
}

// Lookup resolves a recipient to its destinations; a +tag suffix on the local part still matches the base alias.
func (r Rules) Lookup(addr string) ([]string, bool) {
	local, domain, ok := splitAddr(addr)
	if !ok {
		return nil, false
	}
	aliases, ok := r[domain]
	if !ok {
		return nil, false
	}
	local = strings.ToLower(local)
	if dst, ok := aliases[local]; ok {
		return dst, true
	}
	if i := strings.IndexByte(local, '+'); i > 0 {
		if dst, ok := aliases[local[:i]]; ok {
			return dst, true
		}
	}
	dst, ok := aliases["*"]
	return dst, ok
}

// Resolve follows forwards that land on other forwarded addresses, so local chains resolve here instead of looping back through the network.
func (r Rules) Resolve(addr string) []string {
	var out []string
	seen := map[string]bool{strings.ToLower(addr): true}
	var walk func(addr string, depth int)
	walk = func(addr string, depth int) {
		dst, _ := r.Lookup(addr)
		for _, d := range dst {
			k := strings.ToLower(d)
			if seen[k] {
				continue
			}
			seen[k] = true
			if _, local := r.Lookup(d); local && depth < 8 {
				walk(d, depth+1)
				continue
			}
			out = append(out, d)
		}
	}
	walk(addr, 0)
	return out
}

func (r Rules) HasDomain(domain string) bool {
	_, ok := r[strings.ToLower(domain)]
	return ok
}
