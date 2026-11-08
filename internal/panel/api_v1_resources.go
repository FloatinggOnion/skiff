package panel

import (
	"encoding/json"
	"net/http"

	"github.com/nuelScript/skiff/internal/config"
)

type apiDatabaseView struct {
	Name     string   `json:"name"`
	Engine   string   `json:"engine"`
	State    string   `json:"state"`
	Port     int      `json:"port"`
	Attached []string `json:"attached"`
}

func (p *Panel) toAPIDatabase(d dbRow) apiDatabaseView {
	attached := dbAttachments(d.ID)
	if attached == nil {
		attached = []string{}
	}
	return apiDatabaseView{d.Name, d.Engine, p.eng.State(d.Container), d.Port, attached}
}

func (p *Panel) apiListDatabases(w http.ResponseWriter, r *http.Request) {
	out := []apiDatabaseView{}
	for _, d := range listDBs(apiTeam(r)) {
		out = append(out, p.toAPIDatabase(d))
	}
	apiJSON(w, http.StatusOK, out)
}

func (p *Panel) apiCreateDatabase(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Engine string `json:"engine"`
		Name   string `json:"name"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body) != nil {
		apiErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if _, ok := dbEngines[body.Engine]; !ok {
		apiErr(w, http.StatusBadRequest, "engine must be postgres, mysql, mongodb or redis")
		return
	}
	name := sanitizeName(body.Name)
	if name == "" {
		apiErr(w, http.StatusBadRequest, "name must use lowercase letters, numbers and hyphens")
		return
	}
	for _, d := range listDBs(apiTeam(r)) {
		if d.Name == name {
			apiErr(w, http.StatusConflict, "a database named "+name+" already exists")
			return
		}
	}
	d, err := p.provisionDB(apiTeam(r), body.Engine, name)
	if err != nil {
		apiErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	recordAudit(apiTeam(r), AuditEntry{Actor: apiActor(r), Action: "database.create", Target: name, Detail: body.Engine + " via api"})
	apiJSON(w, http.StatusCreated, p.toAPIDatabase(d))
}

func (p *Panel) apiRemoveDatabase(w http.ResponseWriter, r *http.Request) {
	if !databaseToken(w, r) {
		return
	}
	d, ok := apiDatabase(w, r)
	if !ok {
		return
	}
	p.removeDatabase(d)
	recordAudit(apiTeam(r), AuditEntry{Actor: apiActor(r), Action: "database.delete", Target: d.Name, Detail: d.Engine + " via api"})
	w.WriteHeader(http.StatusNoContent)
}

func (p *Panel) apiAttachDatabase(w http.ResponseWriter, r *http.Request) {
	if !databaseToken(w, r) {
		return
	}
	d, ok := apiDatabase(w, r)
	if !ok {
		return
	}
	src, ok := apiSource(r, sanitizeName(r.PathValue("app")))
	if !ok {
		apiErr(w, http.StatusNotFound, "app not found")
		return
	}
	switch r.Method {
	case http.MethodPut:
		if err := p.attachDatabase(d, src.App); err != nil {
			apiErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		recordAudit(apiTeam(r), AuditEntry{Actor: apiActor(r), Action: "database.attach", Target: d.Name, Detail: src.App + " via api"})
	case http.MethodDelete:
		if p.detachDatabase(d, src) {
			recordAudit(apiTeam(r), AuditEntry{Actor: apiActor(r), Action: "database.detach", Target: d.Name, Detail: src.App + " via api"})
		}
	}
	apiJSON(w, http.StatusOK, p.toAPIDatabase(d))
}

type apiBucketView struct {
	Name     string   `json:"name"`
	State    string   `json:"state"`
	Attached []string `json:"attached"`
}

func (p *Panel) toAPIBucket(b bucketRow) apiBucketView {
	attached := bucketAttachments(b.ID)
	if attached == nil {
		attached = []string{}
	}
	return apiBucketView{b.Name, p.eng.State(b.Container), attached}
}

// apiBucket finds a team's bucket by name, writing the error itself when it can't.
func apiBucket(w http.ResponseWriter, r *http.Request) (bucketRow, bool) {
	name := sanitizeName(r.PathValue("name"))
	var found []bucketRow
	for _, b := range listBuckets(apiTeam(r)) {
		if b.Name == name {
			found = append(found, b)
		}
	}
	switch len(found) {
	case 0:
		apiErr(w, http.StatusNotFound, "bucket not found")
		return bucketRow{}, false
	case 1:
		return found[0], true
	}
	apiErr(w, http.StatusConflict, "more than one bucket is named "+name+", so delete one in the dashboard")
	return bucketRow{}, false
}

func (p *Panel) apiListBuckets(w http.ResponseWriter, r *http.Request) {
	out := []apiBucketView{}
	for _, b := range listBuckets(apiTeam(r)) {
		out = append(out, p.toAPIBucket(b))
	}
	apiJSON(w, http.StatusOK, out)
}

func (p *Panel) apiCreateBucket(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body) != nil {
		apiErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	name := sanitizeName(body.Name)
	if len(name) < 3 {
		apiErr(w, http.StatusBadRequest, "bucket name must be at least 3 lowercase letters, numbers, or hyphens")
		return
	}
	for _, b := range listBuckets(apiTeam(r)) {
		if b.Name == name {
			apiErr(w, http.StatusConflict, "a bucket named "+name+" already exists")
			return
		}
	}
	b, err := p.createBucket(apiTeam(r), name)
	if err != nil {
		apiErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	recordAudit(apiTeam(r), AuditEntry{Actor: apiActor(r), Action: "bucket.create", Target: name, Detail: "via api"})
	apiJSON(w, http.StatusCreated, p.toAPIBucket(b))
}

func (p *Panel) apiRemoveBucket(w http.ResponseWriter, r *http.Request) {
	if !databaseToken(w, r) {
		return
	}
	b, ok := apiBucket(w, r)
	if !ok {
		return
	}
	p.removeBucket(b)
	recordAudit(apiTeam(r), AuditEntry{Actor: apiActor(r), Action: "bucket.delete", Target: b.Name, Detail: "via api"})
	w.WriteHeader(http.StatusNoContent)
}

func (p *Panel) apiAttachBucket(w http.ResponseWriter, r *http.Request) {
	if !databaseToken(w, r) {
		return
	}
	b, ok := apiBucket(w, r)
	if !ok {
		return
	}
	src, ok := apiSource(r, sanitizeName(r.PathValue("app")))
	if !ok {
		apiErr(w, http.StatusNotFound, "app not found")
		return
	}
	switch r.Method {
	case http.MethodPut:
		if err := p.attachBucket(b, src.App); err != nil {
			apiErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		recordAudit(apiTeam(r), AuditEntry{Actor: apiActor(r), Action: "bucket.attach", Target: b.Name, Detail: src.App + " via api"})
	case http.MethodDelete:
		p.detachBucket(b, src.App)
		recordAudit(apiTeam(r), AuditEntry{Actor: apiActor(r), Action: "bucket.detach", Target: b.Name, Detail: src.App + " via api"})
	}
	apiJSON(w, http.StatusOK, p.toAPIBucket(b))
}

func (p *Panel) apiVolumes(w http.ResponseWriter, r *http.Request) {
	src, ok := apiSource(r, sanitizeName(r.PathValue("name")))
	if !ok {
		apiErr(w, http.StatusNotFound, "app not found")
		return
	}
	apiJSON(w, http.StatusOK, p.volumeViews(src))
}

func (p *Panel) apiAddVolume(w http.ResponseWriter, r *http.Request) {
	src, ok := apiSource(r, sanitizeName(r.PathValue("name")))
	if !ok {
		apiErr(w, http.StatusNotFound, "app not found")
		return
	}
	if lock := volumeLock(src); lock != "" {
		apiErr(w, http.StatusBadRequest, lock)
		return
	}
	var v config.Volume
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&v) != nil {
		apiErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	v, problem := newVolume(src.App, v)
	if problem != "" {
		apiErr(w, http.StatusBadRequest, problem)
		return
	}
	if err := addVolume(src.App, v); err != nil {
		apiErr(w, http.StatusInternalServerError, "couldn't save the volume")
		return
	}
	recordAudit(apiTeam(r), AuditEntry{Actor: apiActor(r), Action: "volume.add", Target: src.App, Detail: v.Name + " → " + v.Path + " via api"})
	apiJSON(w, http.StatusCreated, volumeView{Name: v.Name, Path: v.Path})
}

func (p *Panel) apiRemoveVolume(w http.ResponseWriter, r *http.Request) {
	src, ok := apiSource(r, sanitizeName(r.PathValue("name")))
	if !ok {
		apiErr(w, http.StatusNotFound, "app not found")
		return
	}
	if lock := volumeLock(src); lock != "" {
		apiErr(w, http.StatusBadRequest, lock)
		return
	}
	name := r.PathValue("volume")
	if !deleteVolume(src.App, name) {
		apiErr(w, http.StatusNotFound, "volume not found")
		return
	}
	recordAudit(apiTeam(r), AuditEntry{Actor: apiActor(r), Action: "volume.remove", Target: src.App, Detail: name + " via api"})
	w.WriteHeader(http.StatusNoContent)
}
