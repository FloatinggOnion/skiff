package panel

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	tunnelCheckEvery  = 30 * time.Second
	tunnelPingTimeout = 90 * time.Second // a pong queues behind data already sent, so a slow link needs room
	tunnelAuditEvery  = 10 * time.Minute
	tunnelsPerTeam    = 64
	tunnelMaxMessage  = 1 << 20
)

var (
	tunnelAudits   = map[string]time.Time{}
	tunnelAuditsMu sync.Mutex

	openTunnels   = map[string]int{}
	openTunnelsMu sync.Mutex
)

func holdTunnel(team string) (release func(), ok bool) {
	openTunnelsMu.Lock()
	defer openTunnelsMu.Unlock()
	if openTunnels[team] >= tunnelsPerTeam {
		return nil, false
	}
	openTunnels[team]++
	return func() {
		openTunnelsMu.Lock()
		defer openTunnelsMu.Unlock()
		if openTunnels[team]--; openTunnels[team] <= 0 {
			delete(openTunnels, team)
		}
	}, true
}

// databaseToken refuses tokens that were never granted database access, so a token kept in CI can't reach a private database.
func databaseToken(w http.ResponseWriter, r *http.Request) bool {
	if tokenReachesDatabases(bearerToken(r)) {
		return true
	}
	apiErr(w, http.StatusForbidden, "this token can't reach databases, sign in again with `skiff login`")
	return false
}

// auditTunnel records a token reaching a database. Clients open many connections, so repeats inside tunnelAuditEvery are left out.
func auditTunnel(team, actor string, d dbRow) {
	key := team + "\x00" + actor + "\x00" + d.ID
	now := time.Now()
	tunnelAuditsMu.Lock()
	for k, at := range tunnelAudits {
		if now.Sub(at) >= tunnelAuditEvery {
			delete(tunnelAudits, k)
		}
	}
	_, recent := tunnelAudits[key]
	if !recent {
		tunnelAudits[key] = now
	}
	tunnelAuditsMu.Unlock()
	if !recent {
		recordAudit(team, AuditEntry{Actor: actor, Action: "database.connect", Target: d.Name, Detail: d.Engine})
	}
}

// apiDatabase finds a team's database by name, writing the error itself when it can't.
func apiDatabase(w http.ResponseWriter, r *http.Request) (dbRow, bool) {
	name := sanitizeName(r.PathValue("name"))
	var found []dbRow
	for _, d := range listDBs(apiTeam(r)) {
		if d.Name == name {
			found = append(found, d)
		}
	}
	switch len(found) {
	case 0:
		apiErr(w, http.StatusNotFound, "database not found")
		return dbRow{}, false
	case 1:
		return found[0], true
	}
	apiErr(w, http.StatusConflict, "more than one database is named "+name+", so rename one in the dashboard")
	return dbRow{}, false
}

func (p *Panel) apiListDatabases(w http.ResponseWriter, r *http.Request) {
	type view struct {
		Name   string `json:"name"`
		Engine string `json:"engine"`
		State  string `json:"state"`
		Port   int    `json:"port"`
	}
	out := []view{}
	for _, d := range listDBs(apiTeam(r)) {
		out = append(out, view{d.Name, d.Engine, p.eng.State(d.Container), d.Port})
	}
	apiJSON(w, http.StatusOK, out)
}

// apiDatabaseConnection answers with the address a client uses through a local tunnel on ?port=.
func (p *Panel) apiDatabaseConnection(w http.ResponseWriter, r *http.Request) {
	if !databaseToken(w, r) {
		return
	}
	d, ok := apiDatabase(w, r)
	if !ok {
		return
	}
	port, err := strconv.Atoi(r.URL.Query().Get("port"))
	if err != nil || port < 1 || port > 65535 {
		apiErr(w, http.StatusBadRequest, "port must be from 1 to 65535")
		return
	}
	auditTunnel(apiTeam(r), apiActor(r), d)
	apiJSON(w, http.StatusOK, map[string]string{
		"url": dbEngines[d.Engine].url("127.0.0.1", port, d.Username, d.Password, d.DBName, false),
	})
}

// apiDatabaseTunnel carries one client connection to a private database over a WebSocket.
func (p *Panel) apiDatabaseTunnel(w http.ResponseWriter, r *http.Request) {
	if !databaseToken(w, r) {
		return
	}
	d, ok := apiDatabase(w, r)
	if !ok {
		return
	}
	rt, err := p.eng.Runtime(d.Container)
	if err != nil || !rt.Running {
		apiErr(w, http.StatusConflict, d.Name+" isn't running")
		return
	}
	ip := rt.IPs[d.network()]
	if ip == "" {
		apiErr(w, http.StatusConflict, d.Name+" isn't on a network the server can reach")
		return
	}
	release, ok := holdTunnel(apiTeam(r))
	if !ok {
		apiErr(w, http.StatusTooManyRequests, "your team has too many open tunnels, close some and try again")
		return
	}
	defer release()
	upstream, err := net.DialTimeout("tcp", net.JoinHostPort(ip, strconv.Itoa(dbEngines[d.Engine].port)), 5*time.Second)
	if err != nil {
		apiErr(w, http.StatusBadGateway, fmt.Sprintf("%s isn't accepting connections yet", d.Name))
		return
	}
	defer upstream.Close()

	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(tunnelMaxMessage)
	auditTunnel(apiTeam(r), apiActor(r), d)

	// r.Context() ends once the connection is hijacked, so the tunnel keeps its own.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := websocket.NetConn(ctx, c, websocket.MessageBinary)
	go func() {
		_, _ = io.Copy(upstream, client)
		cancel()
	}()
	go func() {
		_, _ = io.Copy(client, upstream)
		cancel()
	}()
	go func() {
		<-ctx.Done()
		_ = upstream.Close()
	}()

	team, token := apiTeam(r), bearerToken(r)
	tick := time.NewTicker(tunnelCheckEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			// A revoked token loses its open connections too, and a laptop that went away stops holding one.
			if t, _, ok := resolveToken(token); !ok || t != team {
				return
			}
			pingCtx, stop := context.WithTimeout(ctx, tunnelPingTimeout)
			err := c.Ping(pingCtx)
			stop()
			if err != nil {
				return
			}
		}
	}
}
