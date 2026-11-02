// Package s3 is a minimal client for S3-compatible object storage: put, get, list, delete and a write check.
package s3

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	Endpoint  string // e.g. https://<account>.r2.cloudflarestorage.com
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	HTTP      *http.Client
}

const unsignedPayload = "UNSIGNED-PAYLOAD"

func (c *Client) Put(ctx context.Context, key string, body io.Reader, size int64) error {
	req, err := c.request(ctx, http.MethodPut, key, body)
	if err != nil {
		return err
	}
	req.ContentLength = size
	return c.do(req, http.StatusOK)
}

// Get opens an object for reading; the caller closes it.
func (c *Client) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	body, _, err := c.Open(ctx, key)
	return body, err
}

// Open is Get with the object's size.
func (c *Client) Open(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	req, err := c.request(ctx, http.MethodGet, key, nil)
	if err != nil {
		return nil, 0, err
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Minute}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		resp.Body.Close()
		return nil, 0, fmt.Errorf("GET %s: %s %s", key, resp.Status, errorCode(string(msg)))
	}
	return resp.Body, resp.ContentLength, nil
}

// IsNotFound reports whether an error from Get or Open means the object doesn't exist.
func IsNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "NoSuchKey")
}

type Object struct {
	Key      string
	Size     int64
	Modified time.Time
}

// Listing is one page of a folder: its objects, the folders inside it, and the token for the next page.
type Listing struct {
	Objects   []Object
	Folders   []string
	NextToken string
}

// List returns one page of what's directly inside prefix, treating "/" as the folder separator.
func (c *Client) List(ctx context.Context, prefix, token string, limit int) (Listing, error) {
	req, err := c.request(ctx, http.MethodGet, "", nil)
	if err != nil {
		return Listing{}, err
	}
	// In canonical order: SigV4 signs the query string sorted by name.
	q := []string{}
	if token != "" {
		q = append(q, "continuation-token="+escapeQuery(token))
	}
	q = append(q, "delimiter=%2F", "list-type=2", "max-keys="+strconv.Itoa(limit), "prefix="+escapeQuery(prefix))
	req.URL.RawQuery = strings.Join(q, "&")
	c.sign(req, time.Now().UTC())
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Minute}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return Listing{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return Listing{}, fmt.Errorf("list: %s %s", resp.Status, errorCode(string(msg)))
	}
	return parseListing(io.LimitReader(resp.Body, 32<<20))
}

func parseListing(r io.Reader) (Listing, error) {
	var x struct {
		Contents []struct {
			Key          string
			Size         int64
			LastModified time.Time
		}
		CommonPrefixes []struct{ Prefix string }
		NextToken      string `xml:"NextContinuationToken"`
	}
	if err := xml.NewDecoder(r).Decode(&x); err != nil {
		return Listing{}, fmt.Errorf("reading the listing: %w", err)
	}
	l := Listing{NextToken: x.NextToken}
	for _, o := range x.Contents {
		l.Objects = append(l.Objects, Object{Key: o.Key, Size: o.Size, Modified: o.LastModified})
	}
	for _, p := range x.CommonPrefixes {
		l.Folders = append(l.Folders, p.Prefix)
	}
	return l, nil
}

func (c *Client) Delete(ctx context.Context, key string) error {
	req, err := c.request(ctx, http.MethodDelete, key, nil)
	if err != nil {
		return err
	}
	return c.do(req, http.StatusNoContent, http.StatusOK)
}

// Check proves the credentials can write to the bucket by putting and removing a small object.
func (c *Client) Check(ctx context.Context, prefix string) error {
	key := strings.TrimSuffix(prefix, "/") + "/.skiff-check"
	key = strings.TrimPrefix(key, "/")
	if err := c.Put(ctx, key, strings.NewReader("ok"), 2); err != nil {
		return err
	}
	return c.Delete(ctx, key)
}

func (c *Client) request(ctx context.Context, method, key string, body io.Reader) (*http.Request, error) {
	base, err := url.Parse(strings.TrimSuffix(c.Endpoint, "/"))
	if err != nil || base.Host == "" || (base.Scheme != "https" && base.Scheme != "http") {
		return nil, fmt.Errorf("endpoint must be an http(s) URL")
	}
	path := base.Path + "/" + c.Bucket + "/" + strings.TrimPrefix(key, "/")
	u := *base
	u.Path = path
	u.RawPath = escapePath(path)
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	c.sign(req, time.Now().UTC())
	return req, nil
}

func (c *Client) do(req *http.Request, ok ...int) error {
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Minute}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	for _, code := range ok {
		if resp.StatusCode == code {
			return nil
		}
	}
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	return fmt.Errorf("%s %s: %s %s", req.Method, req.URL.Path, resp.Status, errorCode(string(msg)))
}

func errorCode(body string) string {
	if i := strings.Index(body, "<Code>"); i >= 0 {
		if j := strings.Index(body[i:], "</Code>"); j > 0 {
			return body[i+6 : i+j]
		}
	}
	return ""
}

func (c *Client) sign(req *http.Request, now time.Time) {
	region := c.Region
	if region == "" {
		region = "us-east-1"
	}
	amzDate := now.Format("20060102T150405Z")
	day := now.Format("20060102")
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", unsignedPayload)

	signed := "host;x-amz-content-sha256;x-amz-date"
	canonical := strings.Join([]string{
		req.Method,
		req.URL.EscapedPath(),
		req.URL.RawQuery,
		"host:" + req.URL.Host + "\n" +
			"x-amz-content-sha256:" + unsignedPayload + "\n" +
			"x-amz-date:" + amzDate + "\n",
		signed,
		unsignedPayload,
	}, "\n")
	scope := day + "/" + region + "/s3/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hexSHA(canonical)

	k := hmacSHA([]byte("AWS4"+c.SecretKey), day)
	k = hmacSHA(k, region)
	k = hmacSHA(k, "s3")
	k = hmacSHA(k, "aws4_request")
	sig := hex.EncodeToString(hmacSHA(k, toSign))
	req.Header.Set("Authorization", fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		c.AccessKey, scope, signed, sig))
}

// escapePath percent-encodes each segment per SigV4 (RFC 3986 unreserved left alone, '/' kept).
func escapePath(p string) string {
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		ch := p[i]
		if ch == '/' || ch == '-' || ch == '_' || ch == '.' || ch == '~' ||
			(ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') {
			b.WriteByte(ch)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", ch)
	}
	return b.String()
}

func escapeQuery(s string) string {
	return strings.ReplaceAll(escapePath(s), "/", "%2F")
}

func hmacSHA(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

func hexSHA(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
