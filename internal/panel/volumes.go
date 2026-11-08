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
	rows, err := sqlDB.Query(`SELECT name, path, owner FROM volumes WHERE app=? ORDER BY created, name`, app)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []config.Volume
	for rows.Next() {
		var v config.Volume
		if rows.Scan(&v.Name, &v.Path, &v.Owner) == nil {
			out = append(out, v)
		}
	}
	return out
}

func addVolume(app string, v config.Volume) error {
	_, err := sqlDB.Exec(`INSERT INTO volumes (app, name, path, created, owner) VALUES (?, ?, ?, ?, ?)`,
		app, v.Name, v.Path, time.Now().Unix(), v.Owner)
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
		name := v.DockerName(src.App)
		if p.eng.EnsureAppVolume(name, v.Holder(src.App), v.Name) != nil {
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
		keep[v.DockerName(src.App)] = true
	}
	for _, name := range p.eng.AppVolumes(src.App) {
		if !keep[name] {
			_ = p.eng.RemoveVolume(name)
			removeVolumeBackups(src.App, strings.TrimPrefix(name, config.VolumeName(src.App, "")))
		}
	}
}

func (p *Panel) removeAppVolumes(app string) {
	removeAppVolumeBackups(app)
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

// volumeKind says who sets an app's volumes: the dashboard for a project, skiff.toml for a CLI app, the parent project for a preview.
func volumeKind(src Source) string {
	switch {
	case src.Origin == originCLI:
		return "cli"
	case src.Parent != "":
		return "preview"
	}
	return "project"
}

// volumeLock explains why an app's volumes can't be changed here, or is empty when they can.
func volumeLock(src Source) string {
	if kind := volumeKind(src); kind != "project" {
		return "this app's volumes are set by " + map[string]string{"cli": "its skiff.toml", "preview": "its project"}[kind]
	}
	return ""
}

func (p *Panel) volumeViews(src Source) []volumeView {
	mounted := map[string]bool{}
	for _, v := range p.mountedVolumes(src.App) {
		mounted[v.Name+"\x00"+v.Path] = true
	}
	sizes := p.volumeSizes()
	out := []volumeView{}
	for _, v := range p.volumeDefs(src) {
		out = append(out, volumeView{
			Name: v.Name, Path: v.Path,
			Size:     sizes[v.DockerName(src.App)],
			Attached: mounted[v.Name+"\x00"+v.Path] || v.Owner != "",
		})
	}
	return out
}

// newVolume checks a volume added by hand against the app's others. Only a stack shares one app's volume with another, so it always belongs to this app.
func newVolume(app string, v config.Volume) (config.Volume, string) {
	v.Name, v.Path, v.Owner = strings.TrimSpace(v.Name), strings.TrimSpace(v.Path), ""
	if err := config.ValidateVolumes(append(listVolumes(app), v)); err != nil {
		return v, err.Error()
	}
	return v, ""
}

func (p *Panel) handleVolumes(w http.ResponseWriter, r *http.Request) {
	app := sanitizeName(r.URL.Query().Get("app"))
	src, ok := getSource(app)
	if !ok || !p.canAccess(r, app) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if lock := volumeLock(src); r.Method != http.MethodGet && lock != "" {
		http.Error(w, lock, http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]any{"kind": volumeKind(src), "volumes": p.volumeViews(src)})

	case http.MethodPost:
		var v config.Volume
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&v); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		v, problem := newVolume(app, v)
		if problem != "" {
			http.Error(w, problem, http.StatusBadRequest)
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
