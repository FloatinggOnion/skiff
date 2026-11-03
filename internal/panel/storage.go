package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nuelScript/skiff/internal/docker"
	"github.com/nuelScript/skiff/internal/seal"
)

// Object storage: a team can provision an S3-compatible bucket, backed by its own
// store container on the team's private network — managed exactly
// like a database (provision, attach, delete). Attaching wires the S3 endpoint
// and credentials into an app's environment; the app reaches the store by name over
// the isolated team net.

const (
	storageImage = "chrislusf/seaweedfs:4.50"
	storagePort  = "8333"
	s3Region     = "us-east-1"
)

var s3EnvKeys = []string{"S3_ENDPOINT", "S3_BUCKET", "S3_ACCESS_KEY", "S3_SECRET_KEY", "S3_REGION"}

// Bucket is a managed object store plus the computed fields the dashboard needs.
type Bucket struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Endpoint  string   `json:"endpoint"`
	Region    string   `json:"region"`
	AccessKey string   `json:"accessKey"`
	SecretKey string   `json:"secretKey"`
	State     string   `json:"state"`
	Attached  []string `json:"attached"`
	Created   int64    `json:"created"`
}

type bucketRow struct {
	ID, Team, Name, Container, AccessKey, SecretKey string
	Created                                         int64
}

func scanBucket(row interface{ Scan(...any) error }) (bucketRow, bool) {
	var b bucketRow
	if row.Scan(&b.ID, &b.Team, &b.Name, &b.Container, &b.AccessKey, &b.SecretKey, &b.Created) != nil {
		return bucketRow{}, false
	}
	_ = seal.OpenAll(&b.AccessKey, &b.SecretKey)
	return b, true
}

func getBucket(id string) (bucketRow, bool) {
	return scanBucket(sqlDB.QueryRow(
		`SELECT id,team,name,container,access_key,secret_key,created FROM buckets WHERE id=?`, id))
}

func allBuckets() []bucketRow {
	rows, err := sqlDB.Query(`SELECT id,team,name,container,access_key,secret_key,created FROM buckets`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []bucketRow
	for rows.Next() {
		if b, ok := scanBucket(rows); ok {
			out = append(out, b)
		}
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

func listBuckets(team string) []bucketRow {
	rows, err := sqlDB.Query(
		`SELECT id,team,name,container,access_key,secret_key,created FROM buckets WHERE team=? ORDER BY created DESC`, team)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []bucketRow
	for rows.Next() {
		if b, ok := scanBucket(rows); ok {
			out = append(out, b)
		}
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

func putBucket(b bucketRow) error {
	access, err := seal.Seal(b.AccessKey)
	if err != nil {
		return err
	}
	secret, err := seal.Seal(b.SecretKey)
	if err != nil {
		return err
	}
	_, err = sqlDB.Exec(
		`INSERT INTO buckets(id,team,name,container,access_key,secret_key,created) VALUES(?,?,?,?,?,?,?)`,
		b.ID, b.Team, b.Name, b.Container, access, secret, b.Created)
	return err
}

func deleteBucketRow(id string) {
	tx, err := sqlDB.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback() //nolint:errcheck
	_, e1 := tx.Exec(`DELETE FROM buckets WHERE id=?`, id)
	_, e2 := tx.Exec(`DELETE FROM bucket_attachments WHERE bucket_id=?`, id)
	if e1 == nil && e2 == nil {
		_ = tx.Commit()
	}
}

func bucketAttachments(id string) []string {
	rows, err := sqlDB.Query(`SELECT app FROM bucket_attachments WHERE bucket_id=? ORDER BY app`, id)
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

// bucketEnv is the S3 environment an attached app receives.
func bucketEnv(b bucketRow) map[string]string {
	return map[string]string{
		"S3_ENDPOINT":   "http://" + b.Container + ":" + storagePort,
		"S3_BUCKET":     b.Name,
		"S3_ACCESS_KEY": b.AccessKey,
		"S3_SECRET_KEY": b.SecretKey,
		"S3_REGION":     s3Region,
	}
}

func (p *Panel) toBucket(b bucketRow) Bucket {
	return Bucket{
		ID: b.ID, Name: b.Name,
		Endpoint:  "http://" + b.Container + ":" + storagePort,
		Region:    s3Region,
		AccessKey: b.AccessKey, SecretKey: b.SecretKey,
		State:    p.eng.State(b.Container),
		Attached: bucketAttachments(b.ID),
		Created:  b.Created,
	}
}

func (p *Panel) canAccessBucket(r *http.Request, id string) (bucketRow, bool) {
	b, ok := getBucket(id)
	if !ok || b.Team == "" {
		return bucketRow{}, false
	}
	s, ok := p.session(r)
	if !ok {
		return bucketRow{}, false
	}
	if _, member := p.auth.Role(s.userID, b.Team); !member {
		return bucketRow{}, false
	}
	return b, true
}

// createBucket runs an S3-compatible store for the team and creates the initial bucket inside it, retrying until the store is up.
func (p *Panel) createBucket(team, name string) (bucketRow, error) {
	id := randToken()[:12]
	container := "skiff-s3-" + id
	accessKey := "skiff-" + randToken()[:12]
	secretKey := randToken() + randToken()
	net := teamNetwork(team)
	if err := p.eng.EnsureNetwork(net); err != nil {
		return bucketRow{}, err
	}
	if _, err := p.eng.RunDatabase(docker.DBRunSpec{
		Name: container, Image: storageImage, Network: net,
		Volume: container + "-data", MountAt: "/data",
		Env: map[string]string{"AWS_ACCESS_KEY_ID": accessKey, "AWS_SECRET_ACCESS_KEY": secretKey},
		// Everything but the S3 port stays on loopback: the store's internal admin ports have no auth.
		Cmd: []string{"server", "-dir=/data", "-ip=127.0.0.1", "-ip.bind=127.0.0.1",
			"-s3", "-s3.port=" + storagePort, "-s3.ip.bind=0.0.0.0"},
		Labels: map[string]string{"skiff.kind": "storage", "skiff.bucket": id, "skiff.team": team},
	}); err != nil {
		return bucketRow{}, err
	}

	if err := p.makeBucket(container, name); err != nil {
		_ = p.eng.Stop(container)
		_ = p.eng.Remove(container)
		_ = p.eng.RemoveVolume(container + "-data")
		return bucketRow{}, fmt.Errorf("bucket didn't come up: %w", err)
	}

	b := bucketRow{
		ID: id, Team: team, Name: name, Container: container,
		AccessKey: accessKey, SecretKey: secretKey, Created: time.Now().Unix(),
	}
	if err := putBucket(b); err != nil {
		return bucketRow{}, err
	}
	return b, nil
}

func (p *Panel) makeBucket(container, bucket string) error {
	shell := func(cmd string, timeout time.Duration) (string, error) {
		var out bytes.Buffer
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		err := p.eng.Exec(ctx, container, []string{"weed", "shell"}, strings.NewReader(cmd+"\n"), &out)
		return out.String(), err
	}
	var last string
	for range 30 {
		out, err := shell("s3.bucket.create -name "+bucket, 30*time.Second)
		last = strings.TrimSpace(out)
		if err == nil && strings.Contains(out, "created bucket") {
			return nil
		}
		// A create that timed out on a cold store can still land, after which every retry says it already exists.
		if list, err := shell("s3.bucket.list", 10*time.Second); err == nil && hasBucket(list, bucket) {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("%s", last)
}

func hasBucket(list, bucket string) bool {
	for _, line := range strings.Split(list, "\n") {
		if f := strings.Fields(line); len(f) > 0 && f[0] == bucket {
			return true
		}
	}
	return false
}

// prewarmStorageImages pulls the storage image so the first bucket doesn't stall.
func (p *Panel) prewarmStorageImages() {
	p.prewarmImages(storageImage)
}

func (p *Panel) handleStorage(w http.ResponseWriter, r *http.Request) {
	team := p.teamID(r)
	if team == "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
		out := []Bucket{}
		for _, b := range listBuckets(team) {
			out = append(out, p.toBucket(b))
		}
		writeJSON(w, out)

	case http.MethodPost:
		var body struct{ Name string }
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		name := sanitizeName(body.Name)
		if len(name) < 3 {
			http.Error(w, "bucket name must be at least 3 lowercase letters, numbers, or hyphens", http.StatusBadRequest)
			return
		}
		b, err := p.createBucket(team, name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		p.audit(r, "bucket.create", name, "")
		writeJSON(w, p.toBucket(b))

	case http.MethodDelete:
		id := sanitizeID(r.URL.Query().Get("id"))
		b, ok := p.canAccessBucket(r, id)
		if !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		for _, app := range bucketAttachments(id) {
			for _, k := range s3EnvKeys {
				deleteEnvVar(app, k)
			}
		}
		_ = p.eng.Stop(b.Container)
		_ = p.eng.Remove(b.Container)
		_ = p.eng.RemoveVolume(b.Container + "-data")
		deleteBucketRow(id)
		p.audit(r, "bucket.delete", b.Name, "")
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleStorageAttach wires a bucket into an app's environment (POST) or unwires
// it (DELETE). The app picks up the S3 env on its next deploy.
func (p *Panel) handleStorageAttach(w http.ResponseWriter, r *http.Request) {
	id := sanitizeID(r.URL.Query().Get("id"))
	b, ok := p.canAccessBucket(r, id)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	app := sanitizeName(r.URL.Query().Get("app"))
	if !p.canAccess(r, app) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodPost:
		if err := p.linkResource(b.Container, teamNetwork(b.Team), app); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for k, v := range bucketEnv(b) {
			if err := upsertEnvVar(app, k, v, false); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		_, _ = sqlDB.Exec(`INSERT INTO bucket_attachments(bucket_id,app) VALUES(?,?)
			ON CONFLICT(bucket_id,app) DO NOTHING`, id, app)
		p.audit(r, "bucket.attach", b.Name, app)
		w.WriteHeader(http.StatusNoContent)

	case http.MethodDelete:
		for _, k := range s3EnvKeys {
			deleteEnvVar(app, k)
		}
		_, _ = sqlDB.Exec(`DELETE FROM bucket_attachments WHERE bucket_id=? AND app=?`, id, app)
		if src, ok := getSource(app); ok {
			p.unlinkResource(b.Container, teamNetwork(b.Team), appNetwork(src), bucketAttachments(id))
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
