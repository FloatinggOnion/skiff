package panel

import (
	"context"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nuelScript/skiff/internal/s3"
)

const (
	bucketPageSize  = 200
	bucketMaxUpload = 5 << 30
)

// bucketClient talks to a bucket's store from the panel, which isn't on the team network: requests are tunnelled into the store's container, so no port is published.
func (p *Panel) bucketClient(b bucketRow) *s3.Client {
	return &s3.Client{
		Endpoint: "http://" + b.Container + ":" + storagePort, Region: s3Region, Bucket: b.Name,
		AccessKey: b.AccessKey, SecretKey: b.SecretKey,
		HTTP: &http.Client{Transport: &http.Transport{
			DisableKeepAlives: true,
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return p.eng.DialContainer(ctx, b.Container, "127.0.0.1", storagePort)
			},
		}},
	}
}

// validObjectKey keeps keys to plain relative paths, so none can climb out of the bucket on the store's filesystem.
func validObjectKey(key string) bool {
	if key == "" || len(key) > 1024 || !utf8.ValidString(key) {
		return false
	}
	for _, r := range key {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

func validObjectPrefix(prefix string) bool {
	return prefix == "" || (strings.HasSuffix(prefix, "/") && validObjectKey(strings.TrimSuffix(prefix, "/")))
}

// usableBucket answers the request itself unless the caller's team owns the bucket and its store is running.
func (p *Panel) usableBucket(w http.ResponseWriter, r *http.Request) (bucketRow, bool) {
	b, ok := p.canAccessBucket(r, sanitizeID(r.URL.Query().Get("id")))
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return bucketRow{}, false
	}
	if state := p.eng.State(b.Container); state != "running" {
		http.Error(w, "this bucket's store isn't running", http.StatusConflict)
		return bucketRow{}, false
	}
	return b, true
}

type bucketObject struct {
	Key      string `json:"key"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
}

func (p *Panel) handleBucketObjects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	prefix := r.URL.Query().Get("prefix")
	if !validObjectPrefix(prefix) {
		http.Error(w, "that isn't a folder in this bucket", http.StatusBadRequest)
		return
	}
	b, ok := p.usableBucket(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	l, err := p.bucketClient(b).List(ctx, prefix, r.URL.Query().Get("token"), bucketPageSize)
	if err != nil {
		http.Error(w, "couldn't list the bucket: "+err.Error(), http.StatusBadGateway)
		return
	}
	objects := make([]bucketObject, 0, len(l.Objects))
	for _, o := range l.Objects {
		// A folder placeholder some S3 tools create isn't a file.
		if !strings.HasSuffix(o.Key, "/") {
			objects = append(objects, bucketObject{Key: o.Key, Size: o.Size, Modified: o.Modified.Unix()})
		}
	}
	folders := l.Folders
	if folders == nil {
		folders = []string{}
	}
	writeJSON(w, map[string]any{"objects": objects, "folders": folders, "next": l.NextToken})
}

func (p *Panel) handleBucketObject(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if !validObjectKey(key) {
		http.Error(w, "file paths can't be empty or contain . or .. folders or control characters", http.StatusBadRequest)
		return
	}
	b, ok := p.usableBucket(w, r)
	if !ok {
		return
	}
	c := p.bucketClient(b)
	switch r.Method {
	case http.MethodGet:
		body, size, err := c.Open(r.Context(), key)
		if s3.IsNotFound(err) {
			http.Error(w, "no such file", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, "couldn't read the file: "+err.Error(), http.StatusBadGateway)
			return
		}
		defer body.Close()
		// Always a download: a stored page opened on the dashboard's origin could act as whoever is signed in.
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "sandbox")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(key)}))
		if size >= 0 {
			w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		}
		_, _ = io.Copy(w, body)

	case http.MethodPut:
		if r.ContentLength < 0 {
			http.Error(w, "the upload needs a known size", http.StatusLengthRequired)
			return
		}
		if r.ContentLength > bucketMaxUpload {
			http.Error(w, "files can be up to 5 GB here; use an S3 tool for larger ones", http.StatusRequestEntityTooLarge)
			return
		}
		body := http.MaxBytesReader(w, r.Body, bucketMaxUpload)
		if err := c.Put(r.Context(), key, body, r.ContentLength); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			http.Error(w, "couldn't save the file: "+err.Error(), http.StatusBadGateway)
			return
		}
		p.audit(r, "bucket.upload", b.Name, key)
		w.WriteHeader(http.StatusNoContent)

	case http.MethodDelete:
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		if err := c.Delete(ctx, key); err != nil {
			http.Error(w, "couldn't delete the file: "+err.Error(), http.StatusBadGateway)
			return
		}
		p.audit(r, "bucket.file.delete", b.Name, key)
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
