package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/nuelScript/skiff/internal/mail"
	"github.com/nuelScript/skiff/internal/registry"
	"github.com/nuelScript/skiff/internal/seal"
)

type AlertConfig struct {
	Email      string `json:"email"`
	SlackURL   string `json:"slackUrl"`
	WebhookURL string `json:"webhookUrl"`
}

func getAlerts(team string) AlertConfig {
	var a AlertConfig
	_ = sqlDB.QueryRow(`SELECT email,slack_url,webhook_url FROM alerts WHERE team=?`, team).
		Scan(&a.Email, &a.SlackURL, &a.WebhookURL)
	_ = seal.OpenAll(&a.SlackURL, &a.WebhookURL)
	return a
}

func setAlerts(team string, a AlertConfig) error {
	slack, err := seal.Seal(a.SlackURL)
	if err != nil {
		return err
	}
	webhook, err := seal.Seal(a.WebhookURL)
	if err != nil {
		return err
	}
	_, err = sqlDB.Exec(`
		INSERT INTO alerts(team,email,slack_url,webhook_url) VALUES(?,?,?,?)
		ON CONFLICT(team) DO UPDATE SET
			email=excluded.email, slack_url=excluded.slack_url, webhook_url=excluded.webhook_url`,
		team, a.Email, slack, webhook)
	return err
}

type alertEvent struct {
	Team   string
	Kind   string // deploy.failed | app.unhealthy | app.oom | app.recovered | error.spike | test
	App    string
	Title  string
	Detail string
}

type channelResult struct {
	Channel string `json:"channel"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
}

func deliver(ev alertEvent) []channelResult {
	cfg := getAlerts(ev.Team)
	var res []channelResult
	try := func(name string, f func() error) {
		if err := f(); err != nil {
			res = append(res, channelResult{name, false, err.Error()})
		} else {
			res = append(res, channelResult{name, true, ""})
		}
	}
	if cfg.Email != "" {
		try("email", func() error { return sendEmail(cfg.Email, ev) })
	}
	if cfg.SlackURL != "" {
		try("slack", func() error { return postSlack(cfg.SlackURL, ev) })
	}
	if cfg.WebhookURL != "" {
		try("webhook", func() error { return postWebhook(cfg.WebhookURL, ev) })
	}
	return res
}

// dispatchAlert delivers ev to a team's channels; call as `go dispatchAlert(...)` — it blocks on network I/O.
func dispatchAlert(ev alertEvent) {
	for _, r := range deliver(ev) {
		if !r.OK {
			log.Printf("alert %s via %s: %s", ev.Kind, r.Channel, r.Error)
		}
	}
}

// dashboardURL is where emails link back to; empty when the panel has no public domain.
var dashboardURL string

func sendEmail(to string, ev alertEvent) error {
	relay := mail.RelayFromEnv()
	if relay == nil {
		return fmt.Errorf("email needs SMTP configured on the server (SKIFF_SMTP_*)")
	}
	l := mail.Letter{Subject: ev.Title, Heading: ev.Title, Body: []string{ev.Detail}}
	if dashboardURL != "" && ev.App != "" {
		project := ev.App
		if src, ok := getSource(ev.App); ok && src.Parent != "" {
			project = src.Parent
		}
		l.Button, l.URL = "Open "+project, dashboardURL+"/projects/"+url.PathEscape(project)
	}
	var team string
	_ = sqlDB.QueryRow(`SELECT name FROM teams WHERE id=?`, ev.Team).Scan(&team)
	if team != "" {
		l.Reason = "You're getting this because " + to + " receives alerts for the " + team + " team. Team owners can change this in Settings, under Alerts."
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	return relay.Send(ctx, to, l)
}

func postSlack(url string, ev alertEvent) error {
	return postJSON(url, map[string]string{"text": slackText(ev)})
}

func postWebhook(url string, ev alertEvent) error {
	return postJSON(url, map[string]any{
		"kind": ev.Kind, "app": ev.App, "team": ev.Team,
		"title": ev.Title, "detail": ev.Detail, "ts": nowUnix(),
	})
}

// alertHTTPClient refuses to dial private/loopback/link-local addresses; the check runs at dial time on the resolved IP, so it also stops DNS-rebinding SSRF via user-supplied webhook URLs.
var alertHTTPClient = publicHTTPClient(8 * time.Second)

// publicHTTPClient only dials public addresses, for requests to URLs a user typed in.
func publicHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout: 5 * time.Second,
				Control: func(_, address string, _ syscall.RawConn) error {
					host, _, err := net.SplitHostPort(address)
					if err != nil {
						return err
					}
					if ip := net.ParseIP(host); isBlockedDialIP(ip) {
						return fmt.Errorf("refusing to connect to non-public address %s", host)
					}
					return nil
				},
			}).DialContext,
		},
	}
}

// extraBlocked are ranges net.IP doesn't call private: shared address space, protocol assignments, and the IPv6 forms that wrap an IPv4 address.
var extraBlocked = func() []*net.IPNet {
	var out []*net.IPNet
	for _, c := range []string{"100.64.0.0/10", "192.0.0.0/24", "64:ff9b::/96", "2002::/16"} {
		_, n, _ := net.ParseCIDR(c)
		out = append(out, n)
	}
	return out
}()

func isBlockedDialIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	for _, n := range extraBlocked {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func postJSON(url string, payload any) error {
	body, _ := json.Marshal(payload)
	cl := alertHTTPClient
	if drainsAllowPrivate() {
		cl = &http.Client{Timeout: 8 * time.Second}
	}
	resp, err := cl.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("http status %d", resp.StatusCode)
	}
	return nil
}

func slackText(ev alertEvent) string {
	icon := map[string]string{
		"deploy.failed": "🚨", "app.unhealthy": "🔴", "app.oom": "🔴", "app.recovered": "✅",
		"error.spike": "⚠️", "test": "🔔",
	}[ev.Kind]
	if icon == "" {
		icon = "🔔"
	}
	s := icon + " *" + ev.Title + "*"
	if ev.Detail != "" {
		s += "\n" + ev.Detail
	}
	return s
}

func nowUnix() int64 { return time.Now().Unix() }

// maskURL keeps a webhook's scheme and host so members can tell where alerts go without the path or key that authorises posting there.
func maskURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "••••"
	}
	return u.Scheme + "://" + u.Host + "/••••"
}

func (p *Panel) handleAlerts(w http.ResponseWriter, r *http.Request) {
	team := p.teamID(r)
	switch r.Method {
	case http.MethodGet:
		cfg := getAlerts(team)
		if !p.isOwner(r) {
			cfg.SlackURL, cfg.WebhookURL = maskURL(cfg.SlackURL), maskURL(cfg.WebhookURL)
		}
		smtpOK := mail.RelayFromEnv() != nil
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			AlertConfig
			SMTP bool `json:"smtp"`
		}{cfg, smtpOK})
	case http.MethodPut:
		if !p.isOwner(r) {
			http.Error(w, "only team owners can change alerts", http.StatusForbidden)
			return
		}
		var body AlertConfig
		_ = json.NewDecoder(r.Body).Decode(&body)
		cfg := AlertConfig{
			Email:      strings.TrimSpace(body.Email),
			SlackURL:   strings.TrimSpace(body.SlackURL),
			WebhookURL: strings.TrimSpace(body.WebhookURL),
		}
		if err := setAlerts(team, cfg); err != nil {
			http.Error(w, "couldn't save", http.StatusInternalServerError)
			return
		}
		p.audit(r, "alerts.update", "", "")
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (p *Panel) handleAlertTest(w http.ResponseWriter, r *http.Request) {
	team := p.teamID(r)
	results := deliver(alertEvent{
		Team: team, Kind: "test",
		Title:  "Test alert from Skiff",
		Detail: "If you're reading this, your alert channel is wired up correctly.",
	})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
}

var (
	healthDown    = map[string]int{}
	healthAlerted = map[string]bool{}
	spikeLast     = map[string]int64{}
)

func isInflight(app string) bool {
	inflightMu.Lock()
	defer inflightMu.Unlock()
	_, ok := inflight[app]
	return ok
}

func (p *Panel) alertLoop() {
	time.Sleep(60 * time.Second) // let apps settle after a restart
	tick := time.NewTicker(60 * time.Second)
	defer tick.Stop()
	for range tick.C {
		guard("alertLoop", func() {
			p.checkHealth()
			p.checkErrorSpikes()
		})
	}
}

// checkHealth needs two straight checks with no running container before it alerts, so a deploy's brief container swap doesn't page; it alerts once on recovery.
func (p *Panel) checkHealth() {
	states, err := p.eng.AppStates()
	if err != nil {
		return
	}
	apps, err := registry.Load()
	if err != nil {
		return
	}
	for name, a := range apps {
		src, ok := getSource(name)
		if !ok || uptimeWatched(src, a) {
			continue
		}
		if isInflight(name) || isSleeping(name) {
			healthDown[name] = 0
			continue
		}
		if states[name] == "running" {
			if healthAlerted[name] {
				go dispatchAlert(alertEvent{Team: src.Team, Kind: "app.recovered", App: name,
					Title: "Recovered: " + name, Detail: "The app has a running container again."})
			}
			healthDown[name] = 0
			healthAlerted[name] = false
			continue
		}
		healthDown[name]++
		if healthDown[name] >= 2 && !healthAlerted[name] {
			healthAlerted[name] = true
			st := states[name]
			if st == "" {
				st = "missing"
			}
			go dispatchAlert(alertEvent{Team: src.Team, Kind: "app.unhealthy", App: name,
				Title:  "App down: " + name,
				Detail: "No running container (state: " + st + ")."})
		}
	}
}

func (p *Panel) checkErrorSpikes() {
	data := readMetricsFile()
	now := time.Now().Unix()
	cutoff := now - 5*60
	for app, buckets := range data.Apps {
		src, ok := getSource(app)
		if !ok {
			continue
		}
		var s5, req int
		for _, b := range buckets {
			if b.T >= cutoff {
				s5 += b.S5
				req += b.Req
			}
		}
		if req < 20 || s5 < 10 {
			continue
		}
		rate := float64(s5) / float64(req)
		if rate < 0.10 {
			continue
		}
		if now-spikeLast[app] < 15*60 {
			continue
		}
		spikeLast[app] = now
		go dispatchAlert(alertEvent{Team: src.Team, Kind: "error.spike", App: app,
			Title:  "5xx spike: " + app,
			Detail: fmt.Sprintf("%d server errors in 5 min — %.0f%% of %d requests.", s5, rate*100, req)})
	}
}
