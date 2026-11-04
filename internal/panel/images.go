package panel

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nuelScript/skiff/internal/config"
	"github.com/nuelScript/skiff/internal/seal"
)

func getRegistryCreds(app string) (user, pass string, ok bool) {
	err := sqlDB.QueryRow(`SELECT username,password FROM registry_creds WHERE app=?`, app).Scan(&user, &pass)
	if err != nil {
		return "", "", false
	}
	_ = seal.OpenAll(&pass)
	return user, pass, true
}

func setRegistryCreds(app, user, pass string) error {
	pass, err := seal.Seal(pass)
	if err != nil {
		return err
	}
	_, err = sqlDB.Exec(`INSERT INTO registry_creds(app,username,password) VALUES(?,?,?)
		ON CONFLICT(app) DO UPDATE SET username=excluded.username, password=excluded.password`, app, user, pass)
	return err
}

func deleteRegistryCreds(app string) {
	_, _ = sqlDB.Exec(`DELETE FROM registry_creds WHERE app=?`, app)
}

// validPort accepts an empty port (use the one the image exposes) or 1-65535.
func validPort(s string) bool {
	if s == "" {
		return true
	}
	n, err := strconv.Atoi(s)
	return err == nil && n >= 1 && n <= 65535
}

// handleImageProject creates a project that runs a prebuilt image and starts its first deploy.
func (p *Panel) handleImageProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Name, Image, Port, Username, Password string
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body)
	name := sanitizeName(body.Name)
	img := strings.TrimSpace(body.Image)
	port := strings.TrimSpace(body.Port)
	user := strings.TrimSpace(body.Username)
	switch {
	case name == "" || name != strings.TrimSpace(body.Name):
		http.Error(w, "name must be lowercase letters, digits or dashes", http.StatusBadRequest)
		return
	case config.ValidateImage(img) != nil:
		http.Error(w, config.ValidateImage(img).Error(), http.StatusBadRequest)
		return
	case !validPort(port):
		http.Error(w, "port must be a number from 1 to 65535", http.StatusBadRequest)
		return
	case user != "" && body.Password == "":
		http.Error(w, "enter the registry password or token", http.StatusBadRequest)
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
	src := Source{App: name, Team: team, Port: port, Image: img}
	if err := putSource(src); err != nil {
		http.Error(w, "couldn't create the project", http.StatusInternalServerError)
		return
	}
	envStage.release(name)
	if user != "" {
		_ = setRegistryCreds(name, user, body.Password)
	}
	id := newDeployID()
	p.audit(r, "deploy", name, "new app from image")
	go p.runDeploy(src, "", "", "", "manual", id)
	writeJSON(w, map[string]string{"app": name, "id": id})
}

// handleRegistry manages the credentials an image project pulls with; the password is never sent back.
func (p *Panel) handleRegistry(w http.ResponseWriter, r *http.Request) {
	app := sanitizeName(r.URL.Query().Get("app"))
	if !p.canAccess(r, app) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	src, ok := getSource(app)
	if !ok || src.Image == "" || src.Parent != "" {
		http.Error(w, "only projects that run an image have registry credentials", http.StatusNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		user, _, set := getRegistryCreds(app)
		writeJSON(w, map[string]any{"username": user, "set": set, "host": config.RegistryHost(src.Image)})
	case http.MethodPut:
		var body struct{ Username, Password string }
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body)
		user := strings.TrimSpace(body.Username)
		if user == "" || body.Password == "" {
			http.Error(w, "enter both the username and the password or token", http.StatusBadRequest)
			return
		}
		if err := setRegistryCreds(app, user, body.Password); err != nil {
			http.Error(w, "couldn't save the credentials", http.StatusInternalServerError)
			return
		}
		p.audit(r, "registry.update", app, "")
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		deleteRegistryCreds(app)
		p.audit(r, "registry.delete", app, "")
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
