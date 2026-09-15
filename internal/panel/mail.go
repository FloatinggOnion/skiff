package panel

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type MailForward struct {
	ID           string   `json:"id"`
	Domain       string   `json:"domain"`
	Alias        string   `json:"alias"`
	Destinations []string `json:"destinations"`
	Created      int64    `json:"created"`
}

type MailDomain struct {
	Domain  string   `json:"domain"`
	MX      bool     `json:"mx"`
	SPF     bool     `json:"spf"`
	MXHosts []string `json:"mxHosts,omitempty"`
}

type mailResponse struct {
	MXHost   string        `json:"mxHost"`
	ServerIP string        `json:"serverIp"`
	Relay    bool          `json:"relay"`
	Domains  []MailDomain  `json:"domains"`
	Forwards []MailForward `json:"forwards"`
}

const maxDestinations = 10

var (
	aliasRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]{0,62}[a-z0-9])?$`)
	localRe = regexp.MustCompile(`^[^\s@<>(),;:"\\]{1,64}$`)
)

func mailFilePath() string { return filepath.Join(skiffDir(), "mail.json") }

func (p *Panel) mxHost() string { return "mx." + p.domain }

// writeMailFile mirrors the forwarding table for the router, swapping it in atomically so a half-written file is never read as "forward nothing".
func writeMailFile() {
	rows, err := sqlDB.Query(`SELECT domain, alias, destinations FROM mail_forwards`)
	if err != nil {
		return
	}
	defer rows.Close()
	m := map[string]map[string][]string{}
	for rows.Next() {
		var domain, alias, dst string
		if rows.Scan(&domain, &alias, &dst) != nil {
			continue
		}
		if m[domain] == nil {
			m[domain] = map[string][]string{}
		}
		m[domain][alias] = strings.Split(dst, ",")
	}
	if rows.Err() != nil {
		return
	}
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	path := mailFilePath()
	tmp := path + ".tmp"
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, path)
	}
}

func teamForwards(team string) []MailForward {
	rows, err := sqlDB.Query(
		`SELECT id, domain, alias, destinations, created FROM mail_forwards WHERE team=? ORDER BY domain, alias`, team)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []MailForward{}
	for rows.Next() {
		var f MailForward
		var dst string
		if rows.Scan(&f.ID, &f.Domain, &f.Alias, &dst, &f.Created) == nil {
			f.Destinations = strings.Split(dst, ",")
			out = append(out, f)
		}
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

func deleteTeamMail(team string) {
	if res, err := sqlDB.Exec(`DELETE FROM mail_forwards WHERE team=?`, team); err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			writeMailFile()
		}
	}
}

func normalizeMailDomain(s string) (string, bool) {
	d := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".")
	return d, d != "" && len(d) <= 253 && hostRe.MatchString(d)
}

func normalizeAlias(s string) (string, bool) {
	a := strings.ToLower(strings.TrimSpace(s))
	return a, a == "*" || aliasRe.MatchString(a)
}

func normalizeDestination(s string) (string, bool) {
	s = strings.TrimSpace(s)
	i := strings.LastIndexByte(s, '@')
	if i <= 0 {
		return "", false
	}
	domain, ok := normalizeMailDomain(s[i+1:])
	if !ok || !localRe.MatchString(s[:i]) {
		return "", false
	}
	return s[:i] + "@" + domain, true
}

func (p *Panel) isInstanceDomain(domain string) bool {
	return p.domain != "" && (domain == p.domain || strings.HasSuffix(domain, "."+p.domain))
}

func (p *Panel) handleMail(w http.ResponseWriter, r *http.Request) {
	team := p.teamID(r)
	if team == "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
		forwards := teamForwards(team)
		ip := serverPublicIP(p.domain)
		_, relay := smtpConfig()
		writeJSON(w, mailResponse{
			MXHost:   p.mxHost(),
			ServerIP: ip,
			Relay:    relay,
			Domains:  checkMailDomains(forwards, p.mxHost(), ip),
			Forwards: forwards,
		})

	case http.MethodPost:
		var body struct {
			Domain       string
			Alias        string
			Destinations []string
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		domain, ok := normalizeMailDomain(body.Domain)
		if !ok {
			http.Error(w, "enter a valid domain", http.StatusBadRequest)
			return
		}
		alias, ok := normalizeAlias(body.Alias)
		if !ok {
			http.Error(w, "use letters, numbers, dots, hyphens, or underscores for the address — or * to catch everything", http.StatusBadRequest)
			return
		}
		// The instance's own domain serves every team, so only an owner may take mail for it.
		if p.isInstanceDomain(domain) && !p.isOwner(r) {
			http.Error(w, "only an owner can forward mail for "+p.domain, http.StatusForbidden)
			return
		}
		var dests []string
		seen := map[string]bool{}
		for _, d := range body.Destinations {
			if strings.TrimSpace(d) == "" {
				continue
			}
			dst, ok := normalizeDestination(d)
			if !ok {
				http.Error(w, "“"+strings.TrimSpace(d)+"” isn't a valid email address", http.StatusBadRequest)
				return
			}
			if strings.EqualFold(dst, alias+"@"+domain) {
				http.Error(w, "an address can't forward to itself", http.StatusBadRequest)
				return
			}
			if !seen[strings.ToLower(dst)] {
				seen[strings.ToLower(dst)] = true
				dests = append(dests, dst)
			}
		}
		if len(dests) == 0 || len(dests) > maxDestinations {
			http.Error(w, "forward to between 1 and 10 addresses", http.StatusBadRequest)
			return
		}
		var owner string
		if sqlDB.QueryRow(`SELECT team FROM mail_forwards WHERE domain=? LIMIT 1`, domain).Scan(&owner) == nil && owner != team {
			http.Error(w, "another team already forwards mail for "+domain, http.StatusConflict)
			return
		}
		f := MailForward{ID: randHex(8), Domain: domain, Alias: alias, Destinations: dests, Created: nowUnix()}
		if _, err := sqlDB.Exec(
			`INSERT INTO mail_forwards(id,team,domain,alias,destinations,created) VALUES(?,?,?,?,?,?)`,
			f.ID, team, f.Domain, f.Alias, strings.Join(dests, ","), f.Created); err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				http.Error(w, alias+"@"+domain+" is already forwarded", http.StatusConflict)
				return
			}
			http.Error(w, "could not save the forward", http.StatusInternalServerError)
			return
		}
		writeMailFile()
		p.audit(r, "mail.add", alias+"@"+domain, strings.Join(dests, ", "))
		writeJSON(w, f)

	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		var domain, alias string
		if sqlDB.QueryRow(`SELECT domain, alias FROM mail_forwards WHERE id=? AND team=?`, id, team).Scan(&domain, &alias) != nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = sqlDB.Exec(`DELETE FROM mail_forwards WHERE id=? AND team=?`, id, team)
		writeMailFile()
		p.audit(r, "mail.remove", alias+"@"+domain, "")
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func checkMailDomains(forwards []MailForward, mxHost, serverIP string) []MailDomain {
	var names []string
	seen := map[string]bool{}
	for _, f := range forwards {
		if !seen[f.Domain] {
			seen[f.Domain] = true
			names = append(names, f.Domain)
		}
	}
	sort.Strings(names)
	out := make([]MailDomain, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = checkMailDomain(name, mxHost, serverIP)
		}()
	}
	wg.Wait()
	return out
}

func checkMailDomain(domain, mxHost, serverIP string) MailDomain {
	d := MailDomain{Domain: domain}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if mxs, err := net.DefaultResolver.LookupMX(ctx, domain); err == nil {
		for _, mx := range mxs {
			host := strings.TrimSuffix(strings.ToLower(mx.Host), ".")
			d.MXHosts = append(d.MXHosts, host)
			if host == mxHost || (serverIP != "" && resolveIPs(host)[serverIP]) {
				d.MX = true
			}
		}
	}
	if txts, err := net.DefaultResolver.LookupTXT(ctx, domain); err == nil {
		d.SPF = spfAuthorizes(txts, serverIP, d.MX)
	}
	return d
}

// spfAuthorizes reports whether a domain's SPF policy lets this server send for it — directly by IP, or via "mx" once MX points here.
func spfAuthorizes(txts []string, serverIP string, mxHere bool) bool {
	for _, t := range txts {
		if !strings.HasPrefix(strings.ToLower(t), "v=spf1") {
			continue
		}
		for _, term := range strings.Fields(strings.ToLower(t)) {
			term = strings.TrimPrefix(term, "+")
			if serverIP != "" && (term == "ip4:"+serverIP || term == "ip4:"+serverIP+"/32") {
				return true
			}
			if mxHere && term == "mx" {
				return true
			}
		}
	}
	return false
}
