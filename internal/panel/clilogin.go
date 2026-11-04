package panel

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	cliLoginTTL  = 10 * time.Minute
	cliMaxActive = 200
)

type cliLogin struct {
	code    string // shown only in the terminal; the person types it into the browser, so a link alone can't approve it
	host    string
	ip      string
	started time.Time
	expires time.Time
	token   string
	team    string
	denied  bool
}

// cliCodeLimiter caps wrong codes per signed-in user, so the short code can't be guessed through the approve page.
var cliCodeLimiter = &loginThrottle{windows: map[string]*failWindow{}, max: 5, window: 15 * time.Minute}

var (
	cliMu     sync.Mutex
	cliLogins = map[string]*cliLogin{} // poll secret → login
)

func cliLoginByCode(code string) (string, *cliLogin) {
	for poll, l := range cliLogins {
		if l.code == code && time.Now().Before(l.expires) {
			return poll, l
		}
	}
	return "", nil
}

// handleCLILogin is the CLI's side of signing in: POST starts it, GET polls until someone approves it in the browser.
func (p *Panel) handleCLILogin(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var body struct{ Host string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		host := strings.TrimSpace(body.Host)
		if len(host) > 60 {
			host = host[:60]
		}
		code := strings.ToUpper(randHex(4))
		poll := randToken()
		cliMu.Lock()
		for k, l := range cliLogins {
			if time.Now().After(l.expires) {
				delete(cliLogins, k)
			}
		}
		// Starting a sign-in needs no account, so bound how many can wait at once.
		if len(cliLogins) >= cliMaxActive {
			cliMu.Unlock()
			http.Error(w, "too many sign-ins in progress; try again shortly", http.StatusTooManyRequests)
			return
		}
		cliLogins[poll] = &cliLogin{code: code, host: host, ip: clientIP(r), started: time.Now(), expires: time.Now().Add(cliLoginTTL)}
		cliMu.Unlock()
		writeJSON(w, map[string]string{"code": code, "poll": poll, "approveUrl": baseURL(r) + "/cli-login"})
	case http.MethodGet:
		poll := r.URL.Query().Get("poll")
		cliMu.Lock()
		l, ok := cliLogins[poll]
		if !ok || time.Now().After(l.expires) {
			cliMu.Unlock()
			http.Error(w, "this sign-in expired; run skiff login again", http.StatusGone)
			return
		}
		if l.denied {
			delete(cliLogins, poll)
			cliMu.Unlock()
			http.Error(w, "sign-in was declined", http.StatusForbidden)
			return
		}
		if l.token == "" {
			cliMu.Unlock()
			w.WriteHeader(http.StatusAccepted)
			return
		}
		token, team := l.token, l.team
		delete(cliLogins, poll)
		cliMu.Unlock()
		writeJSON(w, map[string]string{"token": token, "team": team})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleCLIApprove is the browser's side: a signed-in user sees the pending sign-in and approves or declines it for their current team.
func (p *Panel) handleCLIApprove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s, _ := p.session(r)
	if cliCodeLimiter.blocked(s.userID, time.Now()) {
		http.Error(w, "too many wrong codes — try again later", http.StatusTooManyRequests)
		return
	}
	code := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("code")))
	cliMu.Lock()
	defer cliMu.Unlock()
	_, l := cliLoginByCode(code)
	if l == nil || l.token != "" {
		cliCodeLimiter.fail(s.userID, time.Now())
		http.Error(w, "no pending sign-in with that code", http.StatusNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]any{"code": l.code, "host": l.host, "ip": l.ip, "requested": l.started.Unix()})
	case http.MethodPost:
		if r.URL.Query().Get("deny") == "1" {
			l.denied = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		team := s.teamID
		if team == "" {
			http.Error(w, "pick a team first", http.StatusForbidden)
			return
		}
		name := "cli"
		if l.host != "" {
			name = "cli (" + l.host + ")"
		}
		t, err := createToken(team, name, s.userID)
		if err != nil {
			http.Error(w, "couldn't create a token", http.StatusInternalServerError)
			return
		}
		l.token, l.team = t.Token, team
		p.audit(r, "cli.login", name, "")
		w.WriteHeader(http.StatusNoContent)
	}
}

// apiCLIDeploy records a deploy a signed-in CLI just made to this server, under the token's team, and claims the app for that team if no one has.
func (p *Panel) apiCLIDeploy(w http.ResponseWriter, r *http.Request) {
	var body struct {
		App, ID, Commit, Message string
		Port                     int
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil {
		apiErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	name, id := sanitizeName(body.App), sanitizeID(body.ID)
	if name == "" || id == "" {
		apiErr(w, http.StatusBadRequest, "app and id are required")
		return
	}
	team := apiTeam(r)
	src, ok := getSource(name)
	switch {
	case !ok:
		src = Source{App: name, Team: team, Port: "3000", Origin: originCLI, Replicas: 1, ScaleMin: 1, ScaleMax: 1}
		if body.Port > 0 && body.Port <= 65535 {
			src.Port = strconv.Itoa(body.Port)
		}
		if nameReserved(name, team) {
			apiErr(w, http.StatusBadRequest, reservedNameMsg)
			return
		}
		if envStage.heldByOther(name, team, time.Now()) {
			_ = setEnv(name, nil)
		}
		if err := putSource(src); err != nil {
			apiErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		envStage.release(name)
	case src.Team != team:
		apiErr(w, http.StatusConflict, "an app with that name belongs to another team")
		return
	}
	if deployStatus(name, id) == "" {
		addDeploy(Deploy{ID: id, App: name, Commit: shortCommit(body.Commit), Message: body.Message,
			Trigger: originCLI, Status: "live", Started: time.Now().Unix()})
		writeCLIDeployLog(name, id, time.Now())
	}
	recordAudit(team, AuditEntry{Actor: apiActor(r), Action: "deploy", Target: name, Detail: "from the CLI"})
	apiJSON(w, http.StatusOK, map[string]string{"app": name, "url": "https://" + name + "." + p.domain})
}
