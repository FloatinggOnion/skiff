package panel

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
)

// reservedTCPPorts are used by Skiff itself or by the server's own services.
var reservedTCPPorts = map[int]string{22: "SSH", 25: "email", 80: "the web", 443: "the web", 7070: "the dashboard", 7071: "the dashboard"}

func tcpPortProblem(port int, app string, sources []Source, usedByDBs map[int]string) string {
	if port < 1024 || port > 65535 {
		return "use a port from 1024 to 65535"
	}
	if why, ok := reservedTCPPorts[port]; ok {
		return fmt.Sprintf("port %d is used by %s", port, why)
	}
	for _, s := range sources {
		if s.TCPPort == port && s.App != app {
			return fmt.Sprintf("port %d is already used by %s", port, s.App)
		}
	}
	if name, ok := usedByDBs[port]; ok {
		return fmt.Sprintf("port %d is already used by the database %s", port, name)
	}
	return ""
}

func publicDatabasePorts() map[int]string {
	out := map[int]string{}
	rows, err := sqlDB.Query(`SELECT public_port, name FROM databases WHERE public=1`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var port int
		var name string
		if rows.Scan(&port, &name) == nil {
			out[port] = name
		}
	}
	return out
}

// handleTCPPort opens or closes a public port for a project. It reaches the internet directly, so only a team owner can change it.
func (p *Panel) handleTCPPort(w http.ResponseWriter, r *http.Request) {
	app := sanitizeName(r.URL.Query().Get("app"))
	src, ok := getSource(app)
	if !ok || !p.canAccess(r, app) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]any{"port": src.TCPPort, "containerPort": src.Port})
	case http.MethodPut:
		if !p.isOwner(r) {
			http.Error(w, "only a team owner can open a public port", http.StatusForbidden)
			return
		}
		if src.Parent != "" {
			http.Error(w, "previews can't have a public TCP port", http.StatusBadRequest)
			return
		}
		var body struct {
			Port int `json:"port"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body) != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if body.Port != 0 && body.Port != src.TCPPort {
			if problem := tcpPortProblem(body.Port, app, allSources(), publicDatabasePorts()); problem != "" {
				http.Error(w, problem, http.StatusBadRequest)
				return
			}
			// Something else on the server may hold it, and the router would only be able to log that.
			ln, err := net.Listen("tcp", fmt.Sprintf(":%d", body.Port))
			if err != nil {
				http.Error(w, fmt.Sprintf("port %d is in use on this server", body.Port), http.StatusBadRequest)
				return
			}
			_ = ln.Close()
		}
		src.TCPPort = body.Port
		if body.Port != 0 && src.HealthPath == "" {
			src.HealthMode = "tcp"
		} else if body.Port == 0 && src.HealthMode == "tcp" && src.Stack == "" {
			src.HealthMode = ""
		}
		if err := putSource(src); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = writeEdgeFile()
		p.audit(r, "project.tcp-port", app, fmt.Sprint(body.Port))
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
