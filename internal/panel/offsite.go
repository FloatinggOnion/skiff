package panel

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nuelScript/skiff/internal/s3"

	"github.com/nuelScript/skiff/internal/seal"
)

const offsiteRetention = 30

type OffsiteTarget struct {
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	Prefix    string `json:"prefix"`
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey,omitempty"`
	HasSecret bool   `json:"hasSecret"`
}

func (t OffsiteTarget) configured() bool {
	return t.Endpoint != "" && t.Bucket != "" && t.AccessKey != "" && t.SecretKey != ""
}

var offsiteHTTP = offsiteClient()

// offsiteClient only reaches public addresses unless the box's owner opts in with SKIFF_ALLOW_PRIVATE_ENDPOINTS=1, e.g. for storage on their own network.
func offsiteClient() *http.Client {
	if os.Getenv("SKIFF_ALLOW_PRIVATE_ENDPOINTS") == "1" {
		return &http.Client{Timeout: 30 * time.Minute}
	}
	return publicHTTPClient(30 * time.Minute)
}

func (t OffsiteTarget) client() *s3.Client {
	return &s3.Client{
		Endpoint: t.Endpoint, Region: t.Region, Bucket: t.Bucket,
		AccessKey: t.AccessKey, SecretKey: t.SecretKey, HTTP: offsiteHTTP,
	}
}

func getOffsite(team string) OffsiteTarget {
	var t OffsiteTarget
	_ = sqlDB.QueryRow(`SELECT endpoint,region,bucket,prefix,access_key,secret_key FROM backup_targets WHERE team=?`, team).
		Scan(&t.Endpoint, &t.Region, &t.Bucket, &t.Prefix, &t.AccessKey, &t.SecretKey)
	_ = seal.OpenAll(&t.AccessKey, &t.SecretKey)
	return t
}

func setOffsite(team string, t OffsiteTarget) error {
	access, err := seal.Seal(t.AccessKey)
	if err != nil {
		return err
	}
	secret, err := seal.Seal(t.SecretKey)
	if err != nil {
		return err
	}
	_, err = sqlDB.Exec(`INSERT INTO backup_targets(team,endpoint,region,bucket,prefix,access_key,secret_key,updated)
		VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(team) DO UPDATE SET endpoint=excluded.endpoint, region=excluded.region, bucket=excluded.bucket,
			prefix=excluded.prefix, access_key=excluded.access_key, secret_key=excluded.secret_key, updated=excluded.updated`,
		team, t.Endpoint, t.Region, t.Bucket, t.Prefix, access, secret, time.Now().Unix())
	return err
}

func offsiteKey(prefix string, d dbRow, b backupRow) string {
	stamp := time.Unix(b.Created, 0).UTC().Format("20060102T150405Z")
	key := fmt.Sprintf("%s-%s/%s-%s", d.Name, d.ID, stamp, b.File)
	if p := strings.Trim(prefix, "/"); p != "" {
		key = p + "/" + key
	}
	return key
}

func setBackupOffsite(id, status string) {
	_, _ = sqlDB.Exec(`UPDATE backups SET offsite=? WHERE id=?`, status, id)
}

// uploadOffsite copies a local backup to the team's bucket and prunes the oldest copies past retention; a failed upload is recorded on the backup and leaves the local copy alone.
func uploadOffsite(d dbRow, b backupRow) error {
	t := getOffsite(d.Team)
	if !t.configured() {
		return nil
	}
	f, err := os.Open(backupPath(b))
	if err != nil {
		setBackupOffsite(b.ID, "failed")
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		setBackupOffsite(b.ID, "failed")
		return err
	}
	key := offsiteKey(t.Prefix, d, b)
	c := t.client()
	if err := c.Put(context.Background(), key, f, info.Size()); err != nil {
		setBackupOffsite(b.ID, "failed")
		return err
	}
	setBackupOffsite(b.ID, "ok")
	_, _ = sqlDB.Exec(`INSERT OR REPLACE INTO offsite_objects(db_id,key,created) VALUES(?,?,?)`, d.ID, key, b.Created)
	pruneOffsite(c, d.ID, offsiteRetention)
	return nil
}

func pruneOffsite(c *s3.Client, dbID string, keep int) {
	rows, err := sqlDB.Query(`SELECT key FROM offsite_objects WHERE db_id=? ORDER BY created DESC`, dbID)
	if err != nil {
		return
	}
	var keys []string
	for rows.Next() {
		var k string
		if rows.Scan(&k) == nil {
			keys = append(keys, k)
		}
	}
	rows.Close()
	for i := keep; i < len(keys); i++ {
		if c.Delete(context.Background(), keys[i]) == nil {
			_, _ = sqlDB.Exec(`DELETE FROM offsite_objects WHERE db_id=? AND key=?`, dbID, keys[i])
		}
	}
}

func uploadOffsiteAsync(d dbRow, b backupRow) {
	go guard("uploadOffsite", func() {
		if err := uploadOffsite(d, b); err != nil {
			log.Printf("skiff: offsite copy of %s backup %s failed: %v", d.Name, b.ID, err)
		}
	})
}

// syncLatestOffsite copies each of a team's databases' newest backup that hasn't reached the bucket yet, so turning offsite on protects existing data straight away.
func syncLatestOffsite(team string) {
	for _, d := range listDBs(team) {
		rows := listBackupRows(d.ID)
		if len(rows) == 0 || rows[0].Offsite == "ok" {
			continue
		}
		uploadOffsiteAsync(d, rows[0])
	}
}

func (p *Panel) handleOffsite(w http.ResponseWriter, r *http.Request) {
	team := p.teamID(r)
	if team == "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
		t := getOffsite(team)
		t.HasSecret = t.SecretKey != ""
		t.SecretKey = ""
		writeJSON(w, t)
	case http.MethodPut, http.MethodPost:
		if !p.isOwner(r) {
			http.Error(w, "only team owners can change offsite backups", http.StatusForbidden)
			return
		}
		t, err := offsiteFromBody(r, getOffsite(team))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		if err := t.client().Check(ctx, t.Prefix); err != nil {
			http.Error(w, "couldn't write to that bucket: "+err.Error(), http.StatusBadRequest)
			return
		}
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		was := getOffsite(team)
		if err := setOffsite(team, t); err != nil {
			http.Error(w, "couldn't save", http.StatusInternalServerError)
			return
		}
		if was.Endpoint != t.Endpoint || was.Bucket != t.Bucket || was.Prefix != t.Prefix {
			forgetHistoryCopies(team)
		}
		p.audit(r, "backups.offsite", t.Bucket, "")
		syncLatestOffsite(team)
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		if !p.isOwner(r) {
			http.Error(w, "only team owners can change offsite backups", http.StatusForbidden)
			return
		}
		_, _ = sqlDB.Exec(`DELETE FROM backup_targets WHERE team=?`, team)
		p.audit(r, "backups.offsite", "", "off")
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// offsiteFromBody reads a target from the request; a blank secret keeps the stored one so the form never has to echo it back.
func offsiteFromBody(r *http.Request, current OffsiteTarget) (OffsiteTarget, error) {
	var body OffsiteTarget
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return OffsiteTarget{}, fmt.Errorf("bad request")
	}
	t := OffsiteTarget{
		Endpoint:  strings.TrimSuffix(strings.TrimSpace(body.Endpoint), "/"),
		Region:    strings.TrimSpace(body.Region),
		Bucket:    strings.TrimSpace(body.Bucket),
		Prefix:    strings.Trim(strings.TrimSpace(body.Prefix), "/"),
		AccessKey: strings.TrimSpace(body.AccessKey),
		SecretKey: strings.TrimSpace(body.SecretKey),
	}
	if t.SecretKey == "" {
		t.SecretKey = current.SecretKey
	}
	if !strings.HasPrefix(t.Endpoint, "https://") && !strings.HasPrefix(t.Endpoint, "http://") {
		return OffsiteTarget{}, fmt.Errorf("endpoint must start with https://")
	}
	if !t.configured() {
		return OffsiteTarget{}, fmt.Errorf("endpoint, bucket, access key and secret key are all required")
	}
	return t, nil
}
