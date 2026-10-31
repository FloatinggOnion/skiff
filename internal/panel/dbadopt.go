package panel

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/nuelScript/skiff/internal/docker"
)

type unmanagedDB struct {
	Container string `json:"container"`
	Engine    string `json:"engine"`
	Image     string `json:"image"`
	Volume    string `json:"volume"`
}

// engineForImage maps an image reference like postgres:15 or docker.io/library/redis:7 to one of our engines.
func engineForImage(image string) string {
	repo := image
	if i := strings.LastIndexByte(repo, '@'); i >= 0 {
		repo = repo[:i]
	}
	if i := strings.LastIndexByte(repo, ':'); i > strings.LastIndexByte(repo, '/') {
		repo = repo[:i]
	}
	if i := strings.LastIndexByte(repo, '/'); i >= 0 {
		repo = repo[i+1:]
	}
	switch repo {
	case "postgres", "postgis":
		return "postgres"
	case "mysql":
		return "mysql"
	case "mongo":
		return "mongodb"
	case "redis":
		return "redis"
	}
	return ""
}

type dbCreds struct{ user, pass, dbname string }

// credsFrom reads the credentials a database container was initialised with, following each official image's defaults.
func credsFrom(engine string, rt docker.ContainerRuntime) (dbCreds, bool) {
	env := rt.Env
	switch engine {
	case "postgres":
		c := dbCreds{user: env["POSTGRES_USER"], pass: env["POSTGRES_PASSWORD"], dbname: env["POSTGRES_DB"]}
		if c.user == "" {
			c.user = "postgres"
		}
		if c.dbname == "" {
			c.dbname = c.user
		}
		return c, true
	case "mysql":
		if env["MYSQL_USER"] != "" && env["MYSQL_PASSWORD"] != "" {
			return dbCreds{user: env["MYSQL_USER"], pass: env["MYSQL_PASSWORD"], dbname: env["MYSQL_DATABASE"]}, true
		}
		if env["MYSQL_ROOT_PASSWORD"] != "" {
			return dbCreds{user: "root", pass: env["MYSQL_ROOT_PASSWORD"], dbname: env["MYSQL_DATABASE"]}, true
		}
		return dbCreds{}, false
	case "mongodb":
		c := dbCreds{user: env["MONGO_INITDB_ROOT_USERNAME"], pass: env["MONGO_INITDB_ROOT_PASSWORD"], dbname: env["MONGO_INITDB_DATABASE"]}
		if c.user == "" || c.pass == "" {
			return dbCreds{}, false
		}
		if c.dbname == "" {
			c.dbname = "admin"
		}
		return c, true
	case "redis":
		for i, a := range rt.Cmd {
			if a == "--requirepass" && i+1 < len(rt.Cmd) {
				return dbCreds{pass: rt.Cmd[i+1]}, true
			}
		}
		return dbCreds{}, true
	}
	return dbCreds{}, false
}

func dataVolume(engine string, rt docker.ContainerRuntime) string {
	at := dbEngines[engine].mountAt
	for _, m := range rt.Mounts {
		if m.Destination == at {
			return m.Name
		}
	}
	return ""
}

// unmanagedDatabases finds running database containers nobody created through Skiff, skipping any already adopted.
func (p *Panel) unmanagedDatabases() []unmanagedDB {
	names, err := p.eng.RunningContainers()
	if err != nil {
		return nil
	}
	known := map[string]bool{}
	for _, d := range allDatabases() {
		known[d.Container] = true
	}
	out := []unmanagedDB{}
	for _, name := range names {
		if known[name] {
			continue
		}
		rt, err := p.eng.Runtime(name)
		if err != nil || rt.Labels["skiff.kind"] != "" || rt.Labels["skiff"] != "" {
			continue
		}
		engine := engineForImage(rt.Image)
		if engine == "" {
			continue
		}
		if _, ok := credsFrom(engine, rt); !ok {
			continue
		}
		out = append(out, unmanagedDB{Container: name, Engine: engine, Image: rt.Image, Volume: dataVolume(engine, rt)})
	}
	return out
}

func (p *Panel) handleUnmanagedDatabases(w http.ResponseWriter, r *http.Request) {
	if !p.isInstanceAdmin(r) {
		writeJSON(w, []unmanagedDB{})
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, p.unmanagedDatabases())
	case http.MethodPost:
		p.adoptDatabase(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (p *Panel) adoptDatabase(w http.ResponseWriter, r *http.Request) {
	team := p.teamID(r)
	container := r.URL.Query().Get("container")
	var found *unmanagedDB
	for _, u := range p.unmanagedDatabases() {
		if u.Container == container {
			found = &u
			break
		}
	}
	if team == "" || found == nil {
		http.Error(w, "no unmanaged database by that name", http.StatusNotFound)
		return
	}
	rt, err := p.eng.Runtime(found.Container)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	creds, _ := credsFrom(found.Engine, rt)
	cmd, _ := json.Marshal(rt.Cmd)
	name := sanitizeName(r.URL.Query().Get("name"))
	if name == "" {
		name = sanitizeName(found.Container)
	}
	d := dbRow{
		ID: randToken()[:12], Team: team, Name: name, Engine: found.Engine, Container: found.Container,
		Host: found.Container, Port: dbEngines[found.Engine].port,
		Username: creds.user, Password: creds.pass, DBName: creds.dbname,
		Created: time.Now().Unix(), Image: found.Image, Volume: found.Volume, Cmd: string(cmd),
	}
	if err := putDB(d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	p.audit(r, "database.adopt", name, found.Engine)
	writeJSON(w, p.toDatabase(d))
}
