package panel

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"sync"
	"unicode/utf8"

	"github.com/nuelScript/skiff/internal/gate"
)

var gateMu sync.Mutex

func gateFile() string { return filepath.Join(skiffDir(), "protected.json") }

func previewPasswordHash(app string) string {
	var h string
	_ = sqlDB.QueryRow(`SELECT hash FROM preview_passwords WHERE app=?`, app).Scan(&h)
	return h
}

// writeGateFile tells the router which previews ask for a password.
func writeGateFile() error {
	gateMu.Lock()
	defer gateMu.Unlock()
	apps := gate.Apps{}
	rows, err := sqlDB.Query(`SELECT s.app, pp.hash FROM sources s JOIN preview_passwords pp ON pp.app = s.parent`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var app, hash string
		if rows.Scan(&app, &hash) == nil {
			apps[app] = hash
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return gate.Save(gateFile(), apps)
}

func (p *Panel) handlePreviewPassword(w http.ResponseWriter, r *http.Request) {
	app := sanitizeName(r.URL.Query().Get("app"))
	if !p.canAccess(r, app) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	src, ok := getSource(app)
	if !ok || src.Parent != "" {
		http.Error(w, "unknown project", http.StatusNotFound)
		return
	}
	switch r.Method {
	case http.MethodPut:
		var body struct{ Password string }
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if n := utf8.RuneCountInString(body.Password); n < 8 || len(body.Password) > 72 {
			http.Error(w, "use 8 to 72 characters", http.StatusBadRequest)
			return
		}
		hash, err := gate.Hash(body.Password)
		if err != nil {
			http.Error(w, "couldn't set the password", http.StatusInternalServerError)
			return
		}
		if _, err := sqlDB.Exec(`INSERT INTO preview_passwords(app,hash) VALUES(?,?)
			ON CONFLICT(app) DO UPDATE SET hash=excluded.hash`, app, hash); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		p.audit(r, "previews.password", app, "set")
	case http.MethodDelete:
		_, _ = sqlDB.Exec(`DELETE FROM preview_passwords WHERE app=?`, app)
		p.audit(r, "previews.password", app, "removed")
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := writeGateFile(); err != nil {
		http.Error(w, "saved, but the router couldn't be told: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
