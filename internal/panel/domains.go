package panel

import (
	"context"
	"encoding/json"
	"io"
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

type Domain struct {
	Host       string   `json:"host"`
	App        string   `json:"app"`
	Parent     string   `json:"parent,omitempty"`
	Branch     string   `json:"branch,omitempty"`
	Created    int64    `json:"created"`
	PointsHere bool     `json:"pointsHere"`
	ResolvesTo []string `json:"resolvesTo,omitempty"`
}

type domainsResponse struct {
	ServerIP string   `json:"serverIp"`
	Domains  []Domain `json:"domains"`
}

var hostRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

// normalizeHost cleans input to a bare hostname and rejects invalid ones — including base-domain subdomains, which the router already serves automatically.
func normalizeHost(host, served string) (string, bool) {
	h := strings.ToLower(strings.TrimSpace(host))
	h = strings.TrimPrefix(h, "https://")
	h = strings.TrimPrefix(h, "http://")
	if i := strings.IndexAny(h, "/:"); i >= 0 {
		h = h[:i]
	}
	h = strings.TrimSuffix(h, ".")
	if h == "" || len(h) > 253 || !hostRe.MatchString(h) {
		return "", false
	}
	if h == served || strings.HasSuffix(h, "."+served) {
		return "", false
	}
	return h, true
}

// primaryDomain picks the address to show for a project with several: the bare domain over its www twin, then the shortest.
func primaryDomain(hosts []string) string {
	best := ""
	rank := func(h string) (bool, int, string) { return strings.HasPrefix(h, "www."), len(h), h }
	for _, h := range hosts {
		if best == "" {
			best = h
			continue
		}
		bw, bl, bs := rank(best)
		hw, hl, hs := rank(h)
		if (bw && !hw) || (bw == hw && (hl < bl || (hl == bl && hs < bs))) {
			best = h
		}
	}
	return best
}

// projectDomains lists each of a team's projects' custom domains, primary first. Domains bound to a branch belong to its preview, so they don't count.
func projectDomains(team string) map[string][]string {
	rows, err := sqlDB.Query(`SELECT host, app FROM domains WHERE team=? AND parent='' AND branch=''`, team)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var host, app string
		if rows.Scan(&host, &app) == nil {
			out[app] = append(out[app], host)
		}
	}
	if rows.Err() != nil {
		return nil
	}
	for app, hosts := range out {
		first := primaryDomain(hosts)
		sort.Slice(hosts, func(i, j int) bool {
			if (hosts[i] == first) != (hosts[j] == first) {
				return hosts[i] == first
			}
			return hosts[i] < hosts[j]
		})
		out[app] = hosts
	}
	return out
}

func domainsFilePath() string { return filepath.Join(skiffDir(), "domains.json") }

func writeDomainsFile() {
	rows, err := sqlDB.Query(`SELECT host, app FROM domains`)
	if err != nil {
		return
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var host, app string
		if rows.Scan(&host, &app) == nil {
			m[host] = app
		}
	}
	if rows.Err() != nil {
		return // don't mirror a truncated map to the router
	}
	if b, err := json.Marshal(m); err == nil {
		_ = os.WriteFile(domainsFilePath(), b, 0o644)
	}
}

func teamDomains(team string) []Domain {
	rows, err := sqlDB.Query(
		`SELECT host, app, parent, branch, created FROM domains WHERE team=? ORDER BY app, host`, team)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []Domain{}
	for rows.Next() {
		var d Domain
		if rows.Scan(&d.Host, &d.App, &d.Parent, &d.Branch, &d.Created) == nil {
			out = append(out, d)
		}
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

func domainOwner(host string) (Domain, bool) {
	var d Domain
	err := sqlDB.QueryRow(`SELECT host, app FROM domains WHERE host=?`, host).Scan(&d.Host, &d.App)
	return d, err == nil
}

// deleteAppDomains removes only plain custom domains of a torn-down app (empty parent); managed branch domains survive to re-bind on the preview's next deploy. Reports whether anything was removed.
func deleteAppDomains(app string) bool {
	res, err := sqlDB.Exec(`DELETE FROM domains WHERE app=? AND parent=''`, app)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// rebindBranchDomains repoints managed domains for (parent, branch) at the preview app, so a branch's hostnames follow its preview across (re)deploys — including one added before the preview existed.
func rebindBranchDomains(parent, branch, app string) bool {
	res, err := sqlDB.Exec(`UPDATE domains SET app=? WHERE parent=? AND branch=?`, app, parent, branch)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

func (p *Panel) handleDomains(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		domains := teamDomains(p.teamID(r))
		ip := serverPublicIP(p.domain)
		resolveDomains(domains, ip)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(domainsResponse{ServerIP: ip, Domains: domains})

	case http.MethodPost:
		var body struct{ App, Host, Branch string }
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		app := sanitizeName(body.App)
		if !p.canAccess(r, app) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		parent, branch, target := "", strings.TrimSpace(body.Branch), app
		if branch != "" {
			src, ok := getSource(app)
			if !ok || src.Parent != "" {
				http.Error(w, "unknown project", http.StatusNotFound)
				return
			}
			name := previewName(app, branch)
			if name == "" || name == app {
				http.Error(w, "couldn't derive a preview name from that branch", http.StatusBadRequest)
				return
			}
			parent, target = app, name
		}
		host, ok := normalizeHost(body.Host, p.domain)
		if !ok {
			http.Error(w, "enter a valid domain (not a "+p.domain+" subdomain)", http.StatusBadRequest)
			return
		}
		if _, taken := domainOwner(host); taken {
			http.Error(w, "that domain is already in use", http.StatusConflict)
			return
		}
		if _, err := sqlDB.Exec(
			`INSERT INTO domains(host,app,team,parent,branch,created) VALUES(?,?,?,?,?,?)`,
			host, target, p.teamID(r), parent, branch, time.Now().Unix()); err != nil {
			http.Error(w, "could not add domain", http.StatusInternalServerError)
			return
		}
		writeDomainsFile()
		p.audit(r, "domain.add", host, target)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Domain{
			Host: host, App: target, Parent: parent, Branch: branch, Created: time.Now().Unix()})

	case http.MethodDelete:
		host, ok := normalizeHost(r.URL.Query().Get("host"), p.domain)
		if !ok {
			http.Error(w, "bad host", http.StatusBadRequest)
			return
		}
		owner, exists := domainOwner(host)
		if !exists {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if !p.canAccess(r, owner.App) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		_, _ = sqlDB.Exec(`DELETE FROM domains WHERE host=?`, host)
		writeDomainsFile()
		p.audit(r, "domain.remove", host, owner.App)
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func resolveDomains(domains []Domain, serverIP string) {
	for i := range domains {
		ips := resolveIPs(domains[i].Host)
		domains[i].ResolvesTo = sortedIPs(ips)
		if serverIP != "" && ips[serverIP] {
			domains[i].PointsHere = true
		}
	}
}

func resolveIPs(host string) map[string]bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return nil
	}
	set := make(map[string]bool, len(addrs))
	for _, a := range addrs {
		set[a] = true
	}
	return set
}

func sortedIPs(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for ip := range set {
		out = append(out, ip)
	}
	sort.Strings(out)
	return out
}

var (
	pubIPMu  sync.Mutex
	pubIPVal string
)

func serverPublicIP(base string) string {
	pubIPMu.Lock()
	defer pubIPMu.Unlock()
	if pubIPVal != "" {
		return pubIPVal
	}
	if ip := egressIP(); ip != "" {
		pubIPVal = ip
	} else {
		for ip := range resolveIPs(base) {
			pubIPVal = ip
			break
		}
	}
	return pubIPVal
}

func egressIP() string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.ipify.org", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
	ip := strings.TrimSpace(string(b))
	if net.ParseIP(ip) != nil {
		return ip
	}
	return ""
}
