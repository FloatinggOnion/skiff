package panel

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nuelScript/skiff/internal/config"
	"github.com/nuelScript/skiff/internal/docker"
	"github.com/nuelScript/skiff/internal/registry"
)

func listVolumes(app string) []config.Volume {
	rows, err := sqlDB.Query(`SELECT name, path FROM volumes WHERE app=? ORDER BY created, name`, app)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []config.Volume
	for rows.Next() {
		var v config.Volume
		if rows.Scan(&v.Name, &v.Path) == nil {
			out = append(out, v)
		}
	}
	return out
}

func addVolume(app string, v config.Volume) error {
	_, err := sqlDB.Exec(`INSERT INTO volumes (app, name, path, created) VALUES (?, ?, ?, ?)`,
		app, v.Name, v.Path, time.Now().Unix())
	return err
}

func deleteVolume(app, name string) bool {
	res, err := sqlDB.Exec(`DELETE FROM volumes WHERE app=? AND name=?`, app, name)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// volumeDefs is what an app should mount: a CLI app's come from its skiff.toml (read off the running container), a preview uses its project's set on volumes of its own.
func (p *Panel) volumeDefs(src Source) []config.Volume {
	if src.Origin == originCLI {
		return p.mountedVolumes(src.App)
	}
	owner := src.App
	if src.Parent != "" {
		owner = src.Parent
	}
	return listVolumes(owner)
}

// mountedVolumes reads the volumes the app's running container has, as defined in skiff.toml.
func (p *Panel) mountedVolumes(app string) []config.Volume {
	apps, err := registry.Load()
	if err != nil {
		return nil
	}
	a, ok := apps[app]
	if !ok || a.Container == "" {
		return nil
	}
	rt, err := p.eng.Runtime(a.Container)
	if err != nil {
		return nil
	}
	prefix := config.VolumeName(app, "")
	var out []config.Volume
	for _, m := range rt.Mounts {
		if name, ok := strings.CutPrefix(m.Name, prefix); ok {
			out = append(out, config.Volume{Name: name, Path: m.Destination})
		}
	}
	return out
}

// appMounts is the volume set for containers the panel starts itself (scale-ups, workers, jobs), so they see the same files as the app.
func (p *Panel) appMounts(src Source) []docker.Mount {
	var out []docker.Mount
	for _, v := range p.volumeDefs(src) {
		name := config.VolumeName(src.App, v.Name)
		if p.eng.EnsureAppVolume(name, src.App, v.Name) != nil {
			continue
		}
		out = append(out, docker.Mount{Name: name, Destination: v.Path})
	}
	return out
}

// pruneVolumes deletes volumes removed from a project once a deploy has moved its containers off them; CLI apps keep theirs, since their skiff.toml lives elsewhere.
func (p *Panel) pruneVolumes(src Source) {
	if src.Origin == originCLI {
		return
	}
	keep := map[string]bool{}
	for _, v := range p.volumeDefs(src) {
		keep[config.VolumeName(src.App, v.Name)] = true
	}
	for _, name := range p.eng.AppVolumes(src.App) {
		if !keep[name] {
			_ = p.eng.RemoveVolume(name)
		}
	}
}

func (p *Panel) removeAppVolumes(app string) {
	for _, name := range p.eng.AppVolumes(app) {
		_ = p.eng.RemoveVolume(name)
	}
}

var volSizes struct {
	sync.Mutex
	at    time.Time
	sizes map[string]int64
}

func (p *Panel) volumeSizes() map[string]int64 {
	volSizes.Lock()
	defer volSizes.Unlock()
	if volSizes.sizes == nil || time.Since(volSizes.at) > time.Minute {
		volSizes.sizes = p.eng.VolumeSizes()
		volSizes.at = time.Now()
	}
	return volSizes.sizes
}

type volumeView struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Attached bool   `json:"attached"`
}

func (p *Panel) handleVolumes(w http.ResponseWriter, r *http.Request) {
	app := sanitizeName(r.URL.Query().Get("app"))
	src, ok := getSource(app)
	if !ok || !p.canAccess(r, app) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	kind := "project"
	switch {
	case src.Origin == originCLI:
		kind = "cli"
	case src.Parent != "":
		kind = "preview"
	}
	if r.Method != http.MethodGet && kind != "project" {
		http.Error(w, "this app's volumes are set by "+map[string]string{"cli": "its skiff.toml", "preview": "its project"}[kind], http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		mounted := map[string]bool{}
		for _, v := range p.mountedVolumes(app) {
			mounted[v.Name+"\x00"+v.Path] = true
		}
		sizes := p.volumeSizes()
		out := []volumeView{}
		for _, v := range p.volumeDefs(src) {
			out = append(out, volumeView{
				Name: v.Name, Path: v.Path,
				Size:     sizes[config.VolumeName(app, v.Name)],
				Attached: mounted[v.Name+"\x00"+v.Path],
			})
		}
		writeJSON(w, map[string]any{"kind": kind, "volumes": out})

	case http.MethodPost:
		var v config.Volume
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&v); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		v.Name, v.Path = strings.TrimSpace(v.Name), strings.TrimSpace(v.Path)
		if err := config.ValidateVolumes(append(listVolumes(app), v)); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := addVolume(app, v); err != nil {
			http.Error(w, "couldn't save the volume", http.StatusInternalServerError)
			return
		}
		p.audit(r, "volume.add", app, v.Name+" → "+v.Path)
		w.WriteHeader(http.StatusCreated)

	case http.MethodDelete:
		name := r.URL.Query().Get("name")
		if !deleteVolume(app, name) {
			http.Error(w, "no such volume", http.StatusNotFound)
			return
		}
		p.audit(r, "volume.remove", app, name)
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
