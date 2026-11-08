package panel

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/nuelScript/skiff/internal/config"
)

// recipe is a one-click app: an image with what it needs to run, from a file in catalog/. Env values may use {{secret}}, {{url}}, {{host}} and, with a database, {{db.url}}, {{db.host}}, {{db.port}}, {{db.user}}, {{db.password}} and {{db.name}}.
type recipe struct {
	ID       string `json:"id" toml:"-"` // the file's name
	Name     string `json:"name" toml:"name"`
	Blurb    string `json:"blurb" toml:"blurb"`
	Category string `json:"category" toml:"category"`
	Image    string `json:"image" toml:"image"`
	// Memory is roughly what it needs to run, including any database, to show before installing.
	Memory string `json:"memory" toml:"memory"`
	// Database is the managed engine it needs, if any.
	Database string `json:"database,omitempty" toml:"database"`
	// Setup is the first thing to do after installing; on most of these the first visitor becomes the admin.
	Setup string `json:"setup" toml:"setup"`

	Port    int               `json:"-" toml:"port"`
	Timeout int               `json:"-" toml:"timeout"` // seconds to wait for a first start, which can include migrations
	Volumes []config.Volume   `json:"-" toml:"volumes"`
	Env     map[string]string `json:"-" toml:"env"`
	DBVar   string            `json:"-" toml:"database_var"` // the env var that holds the database connection, to record the attachment
}

//go:embed catalog/*.toml
var catalogFiles embed.FS

// catalog is every one-click app, by name. TestCatalogRecipesAreValid checks each file can be installed as written.
var catalog = mustLoadCatalog(catalogFiles)

func mustLoadCatalog(fsys fs.FS) []recipe {
	out, err := loadCatalog(fsys)
	if err != nil {
		panic(err)
	}
	return out
}

// loadCatalog reads every recipe, refusing a key it doesn't know so a typo can't quietly drop a setting.
func loadCatalog(fsys fs.FS) ([]recipe, error) {
	entries, err := fs.Glob(fsys, "catalog/*.toml")
	if err != nil {
		return nil, err
	}
	out := make([]recipe, 0, len(entries))
	for _, path := range entries {
		var r recipe
		md, err := toml.DecodeFS(fsys, path, &r)
		if err != nil {
			return nil, fmt.Errorf("catalog: %s: %w", path, err)
		}
		if extra := md.Undecoded(); len(extra) > 0 {
			return nil, fmt.Errorf("catalog: %s: unknown keys %v", path, extra)
		}
		r.ID = strings.TrimSuffix(filepath.Base(path), ".toml")
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b recipe) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return out, nil
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
		if nameReserved(name, team) {
			http.Error(w, reservedNameMsg, http.StatusBadRequest)
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
		App: name, Team: team, Image: rec.Image, Port: strconv.Itoa(rec.Port),
		HealthTimeout: rec.Timeout, Catalog: rec.ID,
	}
	if err := putSource(src); err != nil {
		undo()
		return "", err
	}
	if err := setEnv(name, renderRecipeEnv(rec.Env, "https://"+name+"."+p.domain, db, randToken)); err != nil {
		undo()
		return "", err
	}
	for _, v := range rec.Volumes {
		if err := addVolume(name, v); err != nil {
			undo()
			return "", err
		}
	}
	if row.ID != "" {
		_ = attachDB(row.ID, name, rec.DBVar)
	}
	id := newDeployID()
	go p.runDeploy(src, "", "", "", "manual", id)
	return id, nil
}
