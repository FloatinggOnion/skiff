package panel

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/nuelScript/skiff/internal/docker"
)

const dbNetwork = "skiff"

type dbEngine struct {
	image      string
	port       int
	mountAt    string
	envVar     string
	label      string
	user       string
	hasDB      bool
	container  func(user, pass, dbname string) (map[string]string, []string)
	url        func(host string, port int, user, pass, dbname string, tls bool) string
	shell      func(user, pass, dbname string) []string
	backupExt  string
	dumpCmd    func(user, pass, dbname string) []string
	restoreCmd func(user, pass, dbname string) []string
	tls        *dbTLS
}

type dbTLS struct {
	entrypoint string
	cmd        []string
	hasCerts   bool
}

var dbEngines = map[string]dbEngine{
	"postgres": {
		image: "postgres:16-alpine", port: 5432, mountAt: "/var/lib/postgresql/data",
		envVar: "DATABASE_URL", label: "PostgreSQL", user: "skiff", hasDB: true,
		container: func(user, pass, dbname string) (map[string]string, []string) {
			return map[string]string{"POSTGRES_USER": user, "POSTGRES_PASSWORD": pass, "POSTGRES_DB": dbname}, nil
		},
		url: func(host string, port int, user, pass, dbname string, tls bool) string {
			// Private hop → sslmode=disable (some drivers otherwise demand TLS the container doesn't serve internally); public → require, so it's encrypted.
			mode := "disable"
			if tls {
				mode = "require"
			}
			return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s", user, pass, host, port, dbname, mode)
		},
		shell: func(user, pass, dbname string) []string {
			return []string{"sh", "-c", fmt.Sprintf("PGPASSWORD=%s exec psql -U %s -d %s", pass, user, dbname)}
		},
		backupExt:  ".sql",
		dumpCmd:    func(user, _, dbname string) []string { return []string{"pg_dump", "-U", user, "-d", dbname} },
		restoreCmd: func(user, _, dbname string) []string { return []string{"psql", "-U", user, "-d", dbname} },
		// Fix key perms as root, then hand off to the normal entrypoint with ssl on.
		tls: &dbTLS{
			hasCerts:   true,
			entrypoint: "sh",
			cmd: []string{"-c",
				"cp /skiff-certs/server.crt /skiff-certs/server.key /tmp/ && chmod 600 /tmp/server.key && " +
					"chown postgres:postgres /tmp/server.crt /tmp/server.key && exec docker-entrypoint.sh \"$@\"",
				"skiff", "postgres", "-c", "ssl=on",
				"-c", "ssl_cert_file=/tmp/server.crt", "-c", "ssl_key_file=/tmp/server.key"},
		},
	},
	"mysql": {
		image: "mysql:8", port: 3306, mountAt: "/var/lib/mysql",
		envVar: "DATABASE_URL", label: "MySQL", user: "skiff", hasDB: true,
		container: func(user, pass, dbname string) (map[string]string, []string) {
			return map[string]string{
				"MYSQL_USER": user, "MYSQL_PASSWORD": pass,
				"MYSQL_DATABASE": dbname, "MYSQL_ROOT_PASSWORD": pass,
			}, nil
		},
		url: func(host string, port int, user, pass, dbname string, tls bool) string {
			u := fmt.Sprintf("mysql://%s:%s@%s:%d/%s", user, pass, host, port, dbname)
			if tls {
				u += "?ssl-mode=REQUIRED"
			}
			return u
		},
		shell: func(user, pass, dbname string) []string {
			// MYSQL_PWD avoids the password-on-argv warning the -p flag prints.
			return []string{"sh", "-c", fmt.Sprintf("MYSQL_PWD=%s exec mysql -u %s %s", pass, user, dbname)}
		},
		backupExt: ".sql",
		dumpCmd: func(user, pass, dbname string) []string {
			return []string{"sh", "-c", fmt.Sprintf("MYSQL_PWD=%s mysqldump -u %s %s", pass, user, dbname)}
		},
		restoreCmd: func(user, pass, dbname string) []string {
			return []string{"sh", "-c", fmt.Sprintf("MYSQL_PWD=%s mysql -u %s %s", pass, user, dbname)}
		},
		// MySQL 8 auto-generates certs and serves TLS with no config — the public URL just needs to ask for it.
		tls: &dbTLS{},
	},
	"mongodb": {
		image: "mongo:7", port: 27017, mountAt: "/data/db",
		envVar: "MONGODB_URI", label: "MongoDB", user: "skiff", hasDB: true,
		container: func(user, pass, dbname string) (map[string]string, []string) {
			return map[string]string{
				"MONGO_INITDB_ROOT_USERNAME": user, "MONGO_INITDB_ROOT_PASSWORD": pass,
				"MONGO_INITDB_DATABASE": dbname,
			}, nil
		},
		url: func(host string, port int, user, pass, dbname string, tls bool) string {
			// Root user authenticates against admin, so authSource=admin is required.
			u := fmt.Sprintf("mongodb://%s:%s@%s:%d/%s?authSource=admin", user, pass, host, port, dbname)
			if tls {
				u += "&tls=true"
			}
			return u
		},
		shell: func(user, pass, dbname string) []string {
			return []string{"sh", "-c", fmt.Sprintf("exec mongosh --quiet -u %s -p %s --authenticationDatabase admin %s", user, pass, dbname)}
		},
		backupExt: ".archive.gz",
		dumpCmd: func(user, pass, dbname string) []string {
			return []string{"mongodump", "-u", user, "-p", pass, "--authenticationDatabase", "admin", "--db", dbname, "--archive", "--gzip"}
		},
		restoreCmd: func(user, pass, _ string) []string {
			return []string{"mongorestore", "-u", user, "-p", pass, "--authenticationDatabase", "admin", "--archive", "--gzip", "--drop"}
		},
		// preferTLS lets the entrypoint's localhost init connect in the clear while offering TLS to public clients.
		tls: &dbTLS{
			hasCerts:   true,
			entrypoint: "sh",
			cmd: []string{"-c",
				"cp /skiff-certs/server.pem /skiff-certs/server.crt /tmp/ && chmod 600 /tmp/server.pem && " +
					"chown mongodb:mongodb /tmp/server.pem /tmp/server.crt && exec docker-entrypoint.sh \"$@\"",
				"skiff", "mongod", "--tlsMode", "preferTLS",
				"--tlsCertificateKeyFile", "/tmp/server.pem", "--tlsCAFile", "/tmp/server.crt",
				"--tlsAllowConnectionsWithoutCertificates", "--bind_ip_all"},
		},
	},
	"redis": {
		image: "redis:7-alpine", port: 6379, mountAt: "/data",
		envVar: "REDIS_URL", label: "Redis",
		container: func(_, pass, _ string) (map[string]string, []string) {
			return nil, []string{"redis-server", "--requirepass", pass, "--appendonly", "yes"}
		},
		url: func(host string, port int, _, pass, _ string, _ bool) string {
			// The "default" ACL user is what requirepass sets the password for; the empty-username form (redis://:pass@) is rejected as WRONGPASS on Redis 7.
			return fmt.Sprintf("redis://default:%s@%s:%d", pass, host, port)
		},
		shell: func(_, pass, _ string) []string {
			if pass == "" {
				return []string{"redis-cli"}
			}
			return []string{"sh", "-c", fmt.Sprintf("exec redis-cli -a %s", pass)}
		},
	},
}

type Database struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Engine    string   `json:"engine"`
	Host      string   `json:"host"`
	Port      int      `json:"port"`
	Created   int64    `json:"created"`
	State     string   `json:"state"`
	URL       string   `json:"url"`
	Attached  []string `json:"attached"`
	Public    bool     `json:"public"`
	PublicURL string   `json:"publicUrl,omitempty"`
}

type dbRow struct {
	ID, Team, Name, Engine, Container, Host string
	Port                                    int
	Username, Password, DBName              string
	Created                                 int64
	Public                                  bool
	PublicPort                              int
	Image, Volume                           string // set for adopted databases; else the engine's image and <container>-data
	Cmd                                     string // adopted databases: the JSON command they were started with
}

func (d dbRow) image() string {
	if d.Image != "" {
		return d.Image
	}
	return dbEngines[d.Engine].image
}

func (d dbRow) volume() string {
	if d.Volume != "" {
		return d.Volume
	}
	return d.Container + "-data"
}

const dbCols = `id,team,name,engine,container,host,port,username,password,dbname,created,public,public_port,image,volume,cmd`

func scanDB(row interface{ Scan(...any) error }) (dbRow, bool) {
	var d dbRow
	var public int
	if row.Scan(&d.ID, &d.Team, &d.Name, &d.Engine, &d.Container, &d.Host, &d.Port,
		&d.Username, &d.Password, &d.DBName, &d.Created, &public, &d.PublicPort, &d.Image, &d.Volume, &d.Cmd) != nil {
		return dbRow{}, false
	}
	d.Public = public != 0
	return d, true
}

func getDB(id string) (dbRow, bool) {
	return scanDB(sqlDB.QueryRow(`SELECT `+dbCols+` FROM databases WHERE id=?`, id))
}

func listDBs(team string) []dbRow {
	rows, err := sqlDB.Query(`SELECT `+dbCols+` FROM databases WHERE team=? ORDER BY created DESC`, team)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []dbRow
	for rows.Next() {
		if d, ok := scanDB(rows); ok {
			out = append(out, d)
		}
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

func putDB(d dbRow) error {
	_, err := sqlDB.Exec(`INSERT INTO databases(`+dbCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		d.ID, d.Team, d.Name, d.Engine, d.Container, d.Host, d.Port, d.Username, d.Password,
		d.DBName, d.Created, b2i(d.Public), d.PublicPort, d.Image, d.Volume, d.Cmd)
	return err
}

func deleteDBRow(id string) {
	tx, err := sqlDB.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback() //nolint:errcheck
	_, e1 := tx.Exec(`DELETE FROM databases WHERE id=?`, id)
	_, e2 := tx.Exec(`DELETE FROM db_attachments WHERE db_id=?`, id)
	if e1 == nil && e2 == nil {
		_ = tx.Commit()
	}
}

func dbAttachments(id string) []string {
	rows, err := sqlDB.Query(`SELECT app FROM db_attachments WHERE db_id=? ORDER BY app`, id)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var app string
		if rows.Scan(&app) == nil {
			out = append(out, app)
		}
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

func attachDB(id, app, varName string) error {
	_, err := sqlDB.Exec(`INSERT INTO db_attachments(db_id,app,var) VALUES(?,?,?)
		ON CONFLICT(db_id,app) DO UPDATE SET var=excluded.var`, id, app, varName)
	return err
}

func detachDB(id, app string) (string, bool) {
	var v string
	if sqlDB.QueryRow(`SELECT var FROM db_attachments WHERE db_id=? AND app=?`, id, app).Scan(&v) != nil {
		return "", false
	}
	_, _ = sqlDB.Exec(`DELETE FROM db_attachments WHERE db_id=? AND app=?`, id, app)
	return v, true
}

func upsertEnvVar(app, key, value string, build bool) error {
	_, err := sqlDB.Exec(`INSERT INTO env_vars(app,key,value,build) VALUES(?,?,?,?)
		ON CONFLICT(app,key) DO UPDATE SET value=excluded.value, build=excluded.build`, app, key, value, b2i(build))
	return err
}

func deleteEnvVar(app, key string) {
	_, _ = sqlDB.Exec(`DELETE FROM env_vars WHERE app=? AND key=?`, app, key)
}

func (p *Panel) toDatabase(d dbRow) Database {
	e := dbEngines[d.Engine]
	out := Database{
		ID: d.ID, Name: d.Name, Engine: d.Engine, Host: d.Host, Port: d.Port,
		Created:  d.Created,
		State:    p.eng.State(d.Container),
		URL:      e.url(d.Host, d.Port, d.Username, d.Password, d.DBName, false),
		Attached: dbAttachments(d.ID),
		Public:   d.Public,
	}
	if d.Public && d.PublicPort > 0 {
		if ip := serverPublicIP(p.domain); ip != "" {
			out.PublicURL = e.url(ip, d.PublicPort, d.Username, d.Password, d.DBName, e.tls != nil)
		}
	}
	return out
}

func (p *Panel) canAccessDB(r *http.Request, id string) (dbRow, bool) {
	d, ok := getDB(id)
	if !ok || d.Team == "" {
		return dbRow{}, false
	}
	s, ok := p.session(r)
	if !ok {
		return dbRow{}, false
	}
	if _, member := p.auth.Role(s.userID, d.Team); !member {
		return dbRow{}, false
	}
	return d, true
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (p *Panel) prewarmDatabaseImages() {
	imgs := make([]string, 0, len(dbEngines))
	for _, e := range dbEngines {
		imgs = append(imgs, e.image)
	}
	p.prewarmImages(imgs...)
}

// provisionDB starts a managed database for the team and records it.
func (p *Panel) provisionDB(team, engine, name string) (dbRow, error) {
	e := dbEngines[engine]
	id := randToken()[:12]
	container := "skiff-db-" + id
	pass := randToken()
	user := e.user
	dbname := ""
	if e.hasDB {
		dbname = name
	}
	env, cmd := e.container(user, pass, dbname)
	net := teamNetwork(team)
	if err := p.eng.EnsureNetwork(net); err != nil {
		return dbRow{}, err
	}
	if _, err := p.eng.RunDatabase(docker.DBRunSpec{
		Name: container, Image: e.image, Network: net,
		Volume: container + "-data", MountAt: e.mountAt, Env: env, Cmd: cmd,
		Labels: map[string]string{"skiff.kind": "database", "skiff.db": id, "skiff.team": team},
	}); err != nil {
		return dbRow{}, err
	}
	d := dbRow{
		ID: id, Team: team, Name: name, Engine: engine, Container: container,
		Host: container, Port: e.port, Username: user, Password: pass, DBName: dbname,
		Created: time.Now().Unix(),
	}
	return d, putDB(d)
}

func (p *Panel) handleDatabases(w http.ResponseWriter, r *http.Request) {
	team := p.teamID(r)
	if team == "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
		out := []Database{}
		for _, d := range listDBs(team) {
			out = append(out, p.toDatabase(d))
		}
		writeJSON(w, out)

	case http.MethodPost:
		var body struct{ Engine, Name string }
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		_, ok := dbEngines[body.Engine]
		if !ok {
			http.Error(w, "unknown engine", http.StatusBadRequest)
			return
		}
		name := sanitizeName(body.Name)
		if name == "" {
			http.Error(w, "invalid name", http.StatusBadRequest)
			return
		}
		d, err := p.provisionDB(team, body.Engine, name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		p.audit(r, "database.create", name, body.Engine)
		writeJSON(w, p.toDatabase(d))

	case http.MethodDelete:
		id := sanitizeID(r.URL.Query().Get("id"))
		d, ok := p.canAccessDB(r, id)
		if !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		for _, app := range dbAttachments(id) {
			if v, ok := detachDB(id, app); ok {
				deleteEnvVar(app, v)
			}
		}
		_ = p.eng.Stop(d.Container)
		_ = p.eng.Remove(d.Container)
		_ = p.eng.RemoveVolume(d.volume())
		deleteDBRow(id)
		p.audit(r, "database.delete", d.Name, d.Engine)
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (p *Panel) handleDatabaseAttach(w http.ResponseWriter, r *http.Request) {
	id := sanitizeID(r.URL.Query().Get("id"))
	d, ok := p.canAccessDB(r, id)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	app := sanitizeName(r.URL.Query().Get("app"))
	src, ok := getSource(app)
	if !ok || src.Team != d.Team {
		http.Error(w, "unknown app", http.StatusNotFound)
		return
	}
	e := dbEngines[d.Engine]
	switch r.Method {
	case http.MethodPost:
		url := e.url(d.Host, d.Port, d.Username, d.Password, d.DBName, false)
		if err := upsertEnvVar(app, e.envVar, url, false); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = attachDB(id, app, e.envVar)
		writeJSON(w, p.toDatabase(d))
	case http.MethodDelete:
		if v, ok := detachDB(id, app); ok {
			deleteEnvVar(app, v)
		}
		writeJSON(w, p.toDatabase(d))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (p *Panel) handleDatabasePublic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := sanitizeID(r.URL.Query().Get("id"))
	d, ok := p.canAccessDB(r, id)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	on := r.URL.Query().Get("on") == "1"
	e := dbEngines[d.Engine]
	adopted := d.Image != ""
	if adopted && d.Volume == "" {
		http.Error(w, "this database's data isn't in a named volume, so it can't be recreated safely", http.StatusConflict)
		return
	}
	env, cmd := e.container(d.Username, d.Password, d.DBName)
	net := teamNetwork(d.Team)
	_ = p.eng.EnsureNetwork(net)
	prevNets := []string{}
	if rt, err := p.eng.Runtime(d.Container); err == nil {
		prevNets = rt.Networks
		if adopted {
			env, cmd = rt.Env, nil
			_ = json.Unmarshal([]byte(d.Cmd), &cmd)
		}
	} else if adopted {
		http.Error(w, "couldn't read the database container", http.StatusInternalServerError)
		return
	}
	spec := docker.DBRunSpec{
		Name: d.Container, Image: d.image(), Network: net,
		Volume: d.volume(), MountAt: e.mountAt, Env: env, Cmd: cmd,
		Labels: map[string]string{"skiff.kind": "database", "skiff.db": d.ID, "skiff.team": d.Team},
		Port:   e.port, Publish: on,
	}
	if on && e.tls != nil {
		if e.tls.hasCerts {
			if dir, err := ensureServerCert(); err == nil {
				spec.Binds = []string{dir + ":/skiff-certs:ro"}
			}
		}
		if e.tls.entrypoint != "" {
			spec.Entrypoint = e.tls.entrypoint
			spec.Cmd = e.tls.cmd
		}
	}
	port, err := p.eng.RunDatabase(spec)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Recreating drops the container's other networks; rejoin them so apps that reached it there still can.
	for _, n := range append(prevNets, dbNetwork) {
		if n != net {
			_ = p.eng.ConnectNetwork(n, d.Container)
		}
	}
	if _, err := sqlDB.Exec(`UPDATE databases SET public = ?, public_port = ? WHERE id = ?`,
		b2i(on), port, d.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d.Public, d.PublicPort = on, port
	writeJSON(w, p.toDatabase(d))
}

func (p *Panel) handleDBShell(w http.ResponseWriter, r *http.Request) {
	id := sanitizeID(r.URL.Query().Get("db"))
	d, ok := p.canAccessDB(r, id)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	e := dbEngines[d.Engine]
	p.serveContainerShell(w, r, d.Container, e.shell(d.Username, d.Password, d.DBName))
}
