package panel

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nuelScript/skiff/internal/config"
)

// recipe is a one-click app: an image with what it needs to run. Env values may use {{secret}}, {{url}}, {{host}} and, with a database, {{db.url}}, {{db.host}}, {{db.port}}, {{db.user}}, {{db.password}} and {{db.name}}.
type recipe struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Blurb    string `json:"blurb"`
	Category string `json:"category"`
	Image    string `json:"image"`
	// Memory is roughly what it needs to run, including any database, to show before installing.
	Memory string `json:"memory"`
	// Database is the managed engine it needs, if any.
	Database string `json:"database,omitempty"`
	// Setup is the first thing to do after installing; on most of these the first visitor becomes the admin.
	Setup string `json:"setup"`

	port    int
	timeout int // seconds to wait for a first start, which can include migrations
	volumes []config.Volume
	env     map[string]string
	dbVar   string // the env var that holds the database connection, to record the attachment
}

var catalog = []recipe{
	{
		ID: "uptime-kuma", Name: "Uptime Kuma", Category: "Monitoring",
		Blurb: "Status pages and uptime monitoring for your sites and services.",
		Image: "louislam/uptime-kuma:1", Memory: "256 MB",
		Setup: "Open it right away and create the admin account. The first visitor sets it up.",
		port:  3001, timeout: 120,
		volumes: []config.Volume{{Name: "data", Path: "/app/data"}},
	},
	{
		ID: "n8n", Name: "n8n", Category: "Automation",
		Blurb: "Workflow automation that connects your apps and APIs.",
		Image: "n8nio/n8n:1.100.1", Memory: "512 MB",
		Setup: "Open it right away and create the owner account. The first visitor sets it up.",
		port:  5678, timeout: 180,
		volumes: []config.Volume{{Name: "data", Path: "/home/node/.n8n"}},
		env: map[string]string{
			"N8N_ENCRYPTION_KEY":  "{{secret}}",
			"N8N_HOST":            "{{host}}",
			"N8N_PROTOCOL":        "https",
			"WEBHOOK_URL":         "{{url}}/",
			"N8N_RUNNERS_ENABLED": "true",
			"N8N_PROXY_HOPS":      "1",
		},
	},
	{
		ID: "ghost", Name: "Ghost", Category: "Publishing",
		Blurb: "A blog and newsletter platform, with a MySQL database included.",
		Image: "ghost:5-alpine", Memory: "1 GB", Database: "mysql",
		Setup: "Open /ghost on your site right away and create the owner account. The first visitor sets it up.",
		port:  2368, timeout: 300,
		volumes: []config.Volume{{Name: "content", Path: "/var/lib/ghost/content"}},
		dbVar:   "database__connection__password",
		env: map[string]string{
			"url":                            "{{url}}",
			"database__client":               "mysql",
			"database__connection__host":     "{{db.host}}",
			"database__connection__port":     "{{db.port}}",
			"database__connection__user":     "{{db.user}}",
			"database__connection__password": "{{db.password}}",
			"database__connection__database": "{{db.name}}",
		},
	},
	{
		ID: "umami", Name: "Umami", Category: "Analytics",
		Blurb: "Privacy-friendly website analytics, with a Postgres database included.",
		Image: "ghcr.io/umami-software/umami:postgresql-v2.19.0", Memory: "512 MB", Database: "postgres",
		Setup: "Sign in straight away with username admin and password umami, then change the password. Until you do, anyone can sign in.",
		port:  3000, timeout: 240,
		dbVar: "DATABASE_URL",
		env: map[string]string{
			"DATABASE_URL":  "{{db.url}}",
			"DATABASE_TYPE": "postgresql",
			"APP_SECRET":    "{{secret}}",
		},
	},
	{
		ID: "vaultwarden", Name: "Vaultwarden", Category: "Security",
		Blurb: "A lightweight password manager that works with the Bitwarden apps.",
		Image: "vaultwarden/server:1.34.1", Memory: "128 MB",
		Setup: "Create your account, then set SIGNUPS_ALLOWED to false under Environment and redeploy. Until you do, anyone can sign up.",
		port:  80, timeout: 120,
		volumes: []config.Volume{{Name: "data", Path: "/data"}},
		env:     map[string]string{"DOMAIN": "{{url}}"},
	},
	{
		ID: "gitea", Name: "Gitea", Category: "Development",
		Blurb: "Self-hosted Git with issues, pull requests and a package registry.",
		Image: "gitea/gitea:1.24", Memory: "256 MB",
		Setup: "Open it right away and finish the install screen, creating the admin account there. The first visitor sets it up.",
		port:  3000, timeout: 120,
		volumes: []config.Volume{{Name: "data", Path: "/data"}},
		env: map[string]string{
			"GITEA__server__ROOT_URL":         "{{url}}/",
			"GITEA__server__DISABLE_SSH":      "true",
			"GITEA__database__DB_TYPE":        "sqlite3",
			"GITEA__security__SECRET_KEY":     "{{secret}}",
			"GITEA__security__INTERNAL_TOKEN": "{{secret}}",
		},
	},
}

func catalogRecipe(id string) (recipe, bool) {
	for _, r := range catalog {
		if r.ID == id {
			return r, true
		}
	}
	return recipe{}, false
}

func catalogNote(id string) string {
	r, ok := catalogRecipe(id)
	if !ok {
		return ""
	}
	return r.Setup
}

// recipeDBName avoids a dash in the database's name: some apps' setup scripts don't quote it, and fail on one.
func recipeDBName(app string) string { return strings.ReplaceAll(app, "-", "_") }

// renderRecipeEnv fills in a recipe's env. Each {{secret}} gets its own value.
func renderRecipeEnv(env map[string]string, appURL string, db map[string]string, secret func() string) []EnvVar {
	host := strings.TrimPrefix(strings.TrimPrefix(appURL, "https://"), "http://")
	out := make([]EnvVar, 0, len(env))
	for k, v := range env {
		if strings.Contains(v, "{{secret}}") {
			v = strings.ReplaceAll(v, "{{secret}}", secret())
		}
		v = strings.ReplaceAll(v, "{{url}}", appURL)
		v = strings.ReplaceAll(v, "{{host}}", host)
		for field, value := range db {
			v = strings.ReplaceAll(v, "{{db."+field+"}}", value)
		}
		out = append(out, EnvVar{Key: k, Value: v})
	}
	return out
}

// handleCatalog lists the one-click apps (GET) and installs one (POST).
func (p *Panel) handleCatalog(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, catalog)
	case http.MethodPost:
		var body struct{ ID, Name string }
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body)
		rec, ok := catalogRecipe(body.ID)
		if !ok {
			http.Error(w, "unknown app", http.StatusBadRequest)
			return
		}
		name := sanitizeName(body.Name)
		if name == "" || name != strings.TrimSpace(body.Name) {
			http.Error(w, "name must be lowercase letters, digits or dashes", http.StatusBadRequest)
			return
		}
		team := p.teamID(r)
		if _, exists := getSource(name); exists {
			http.Error(w, "a project with that name already exists", http.StatusConflict)
			return
		}
		if envStage.heldByOther(name, team, time.Now()) {
			_ = setEnv(name, nil)
		}
		id, err := p.installRecipe(rec, name, team)
		if err != nil {
			http.Error(w, "couldn't install "+rec.Name+": "+err.Error(), http.StatusInternalServerError)
			return
		}
		envStage.release(name)
		p.audit(r, "deploy", name, "new app from catalog: "+rec.Name)
		writeJSON(w, map[string]string{"app": name, "id": id})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// installRecipe creates the project and whatever it needs, then starts its first deploy. Nothing is left behind if a step before the deploy fails.
func (p *Panel) installRecipe(rec recipe, name, team string) (string, error) {
	db := map[string]string{}
	var row dbRow
	if rec.Database != "" {
		var err error
		if row, err = p.provisionDB(team, rec.Database, recipeDBName(name)); err != nil {
			return "", fmt.Errorf("starting its database: %w", err)
		}
		e := dbEngines[rec.Database]
		db = map[string]string{
			"url":  e.url(row.Host, row.Port, row.Username, row.Password, row.DBName, false),
			"host": row.Host, "port": strconv.Itoa(row.Port), "user": row.Username,
			"password": row.Password, "name": row.DBName,
		}
	}
	undo := func() {
		deleteSource(name)
		if row.ID != "" {
			_ = p.eng.Stop(row.Container)
			_ = p.eng.Remove(row.Container)
			_ = p.eng.RemoveVolume(row.Container + "-data")
			deleteDBRow(row.ID)
		}
	}
	src := Source{
		App: name, Team: team, Image: rec.Image, Port: strconv.Itoa(rec.port),
		HealthTimeout: rec.timeout, Catalog: rec.ID,
	}
	if err := putSource(src); err != nil {
		undo()
		return "", err
	}
	if err := setEnv(name, renderRecipeEnv(rec.env, "https://"+name+"."+p.domain, db, randToken)); err != nil {
		undo()
		return "", err
	}
	for _, v := range rec.volumes {
		if err := addVolume(name, v); err != nil {
			undo()
			return "", err
		}
	}
	if row.ID != "" {
		_ = attachDB(row.ID, name, rec.dbVar)
	}
	id := newDeployID()
	go p.runDeploy(src, "", "", "", "manual", id)
	return id, nil
}
