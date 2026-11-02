package panel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/smtp"
	"os"
	"strings"
	"syscall"
	"time"

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
	_, err := sqlDB.Exec(`
		INSERT INTO alerts(team,email,slack_url,webhook_url) VALUES(?,?,?,?)
		ON CONFLICT(team) DO UPDATE SET
			email=excluded.email, slack_url=excluded.slack_url, webhook_url=excluded.webhook_url`,
		team, a.Email, a.SlackURL, a.WebhookURL)
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

type smtpSettings struct {
	Host, Addr, User, Pass, From string
}

func smtpConfig() (smtpSettings, bool) {
	host := os.Getenv("SKIFF_SMTP_HOST")
	from := os.Getenv("SKIFF_SMTP_FROM")
	if host == "" || from == "" {
		return smtpSettings{}, false
	}
	port := os.Getenv("SKIFF_SMTP_PORT")
	if port == "" {
		port = "587"
	}
	return smtpSettings{
		Host: host,
		Addr: host + ":" + port,
		User: os.Getenv("SKIFF_SMTP_USER"),
		Pass: os.Getenv("SKIFF_SMTP_PASS"),
		From: from,
	}, true
}

func sendEmail(to string, ev alertEvent) error {
	s, ok := smtpConfig()
	if !ok {
		return fmt.Errorf("email needs SMTP configured on the server (SKIFF_SMTP_*)")
	}
	var auth smtp.Auth
	if s.User != "" {
		auth = smtp.PlainAuth("", s.User, s.Pass, s.Host)
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: Skiff <%s>\r\n", s.From)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: [Skiff] %s\r\n", ev.Title)
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n")
	b.WriteString(ev.Detail + "\r\n")
	return smtp.SendMail(s.Addr, auth, s.From, []string{to}, b.Bytes())
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

func isBlockedDialIP(ip net.IP) bool {
	return ip == nil || ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast()
}

func postJSON(url string, payload any) error {
	body, _ := json.Marshal(payload)
	resp, err := alertHTTPClient.Post(url, "application/json", bytes.NewReader(body))
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

func (p *Panel) handleAlerts(w http.ResponseWriter, r *http.Request) {
	team := p.teamID(r)
	switch r.Method {
	case http.MethodGet:
		cfg := getAlerts(team)
		_, smtpOK := smtpConfig()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			AlertConfig
			SMTP bool `json:"smtp"`
		}{cfg, smtpOK})
	case http.MethodPut:
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
