// Package github integrates Skiff with a GitHub App: manifest creation, installation tokens, repo listing, authenticated clone URLs, and webhook verification.
package github

import (
	"bytes"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Config is the persisted GitHub App (identity, private key, secrets, installation); stored 0600 at ~/.skiff/github.json.
type Config struct {
	ID             int64  `json:"id"`
	Slug           string `json:"slug"`
	PEM            string `json:"pem"`
	WebhookSecret  string `json:"webhook_secret"`
	ClientID       string `json:"client_id"`
	ClientSecret   string `json:"client_secret"`
	InstallationID int64  `json:"installation_id"`
}

func configPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".skiff", "github.json")
}

func Load() *Config {
	b, err := os.ReadFile(configPath())
	if err != nil {
		return nil
	}
	var c Config
	if json.Unmarshal(b, &c) != nil {
		return nil
	}
	return &c
}

func (c *Config) Save() error {
	if err := os.MkdirAll(filepath.Dir(configPath()), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(configPath(), b, 0o600)
}

func (c *Config) Configured() bool { return c != nil && c.ID != 0 }
func (c *Config) Installed() bool  { return c != nil && c.InstallationID != 0 }

func (c *Config) InstallURL() string {
	return fmt.Sprintf("https://github.com/apps/%s/installations/new", c.Slug)
}

func Manifest(baseURL, name string) string {
	m := map[string]any{
		"name":                name,
		"url":                 baseURL,
		"hook_attributes":     map[string]any{"url": baseURL + "/api/github/hook"},
		"redirect_url":        baseURL + "/api/github/created",
		"setup_url":           baseURL + "/api/github/installed",
		"setup_on_update":     true,
		"public":              false,
		"default_permissions": map[string]string{"contents": "read", "metadata": "read", "pull_requests": "write", "statuses": "write"},
		"default_events":      []string{"push", "pull_request"},
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func ConvertManifest(code string) (*Config, error) {
	req, _ := http.NewRequest(http.MethodPost, "https://api.github.com/app-manifests/"+code+"/conversions", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("manifest conversion failed: %s", strings.TrimSpace(string(b)))
	}
	var c Config
	if err := json.NewDecoder(resp.Body).Decode(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) appJWT() (string, error) {
	block, _ := pem.Decode([]byte(c.PEM))
	if block == nil {
		return "", fmt.Errorf("invalid app private key")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		parsed, e2 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if e2 != nil {
			return "", err
		}
		rk, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return "", fmt.Errorf("unexpected private key type")
		}
		key = rk
	}
	now := time.Now()
	header := b64(`{"alg":"RS256","typ":"JWT"}`)
	claims := b64(fmt.Sprintf(`{"iat":%d,"exp":%d,"iss":%d}`,
		now.Add(-30*time.Second).Unix(), now.Add(9*time.Minute).Unix(), c.ID))
	signing := header + "." + claims
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func b64(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

var (
	tokMu  sync.Mutex
	tokVal string
	tokExp time.Time
)

// InstallationToken returns a cached installation access token (1h TTL).
func (c *Config) InstallationToken() (string, error) {
	tokMu.Lock()
	defer tokMu.Unlock()
	if tokVal != "" && time.Until(tokExp) > time.Minute {
		return tokVal, nil
	}
	jwt, err := c.appJWT()
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("https://api.github.com/app/installations/%d/access_tokens", c.InstallationID)
	req, _ := http.NewRequest(http.MethodPost, url, nil)
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("installation token failed: %s", strings.TrimSpace(string(b)))
	}
	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	tokVal, tokExp = out.Token, out.ExpiresAt
	return tokVal, nil
}

type Repo struct {
	FullName      string `json:"full_name"`
	Name          string `json:"name"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
	CloneURL      string `json:"clone_url"`
}

func (c *Config) ListRepos() ([]Repo, error) {
	token, err := c.InstallationToken()
	if err != nil {
		return nil, err
	}
	var repos []Repo
	for page := 1; page <= 10; page++ {
		url := fmt.Sprintf("https://api.github.com/installation/repositories?per_page=100&page=%d", page)
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("Authorization", "token "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode >= 300 {
			resp.Body.Close()
			return nil, fmt.Errorf("listing repositories: github returned %s", resp.Status)
		}
		var out struct {
			Repositories []Repo `json:"repositories"`
		}
		err = json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("listing repositories: %w", err)
		}
		repos = append(repos, out.Repositories...)
		if len(out.Repositories) < 100 {
			break
		}
	}
	return repos, nil
}

func (c *Config) CloneURLWithToken(cloneURL string) (string, error) {
	token, err := c.InstallationToken()
	if err != nil {
		return "", err
	}
	const p = "https://"
	if strings.HasPrefix(cloneURL, p) {
		return p + "x-access-token:" + token + "@" + cloneURL[len(p):], nil
	}
	return cloneURL, nil
}

func VerifySignature(secret string, body []byte, sig string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(want), []byte(sig))
}

type Push struct {
	Repo    string // owner/name
	Branch  string
	Commit  string
	Message string   // head commit message (first line shown in the UI)
	Paths   []string // changed files (best-effort; GitHub truncates very large pushes)
	Deleted bool     // the branch was deleted; only Repo and Branch are set
}

type commitFiles struct {
	Message  string   `json:"message"`
	Added    []string `json:"added"`
	Modified []string `json:"modified"`
	Removed  []string `json:"removed"`
}

func ParsePush(body []byte) (Push, bool) {
	var p struct {
		Ref        string `json:"ref"`
		After      string `json:"after"`
		Deleted    bool   `json:"deleted"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		HeadCommit *commitFiles  `json:"head_commit"`
		Commits    []commitFiles `json:"commits"`
	}
	if json.Unmarshal(body, &p) != nil || p.Repository.FullName == "" || !strings.HasPrefix(p.Ref, "refs/heads/") {
		return Push{}, false
	}
	branch := strings.TrimPrefix(p.Ref, "refs/heads/")
	if p.Deleted {
		return Push{Repo: p.Repository.FullName, Branch: branch, Deleted: true}, true
	}
	seen := map[string]bool{}
	var paths []string
	add := func(fs []string) {
		for _, f := range fs {
			if f != "" && !seen[f] {
				seen[f] = true
				paths = append(paths, f)
			}
		}
	}
	if p.HeadCommit != nil {
		add(p.HeadCommit.Added)
		add(p.HeadCommit.Modified)
		add(p.HeadCommit.Removed)
	}
	for _, c := range p.Commits {
		add(c.Added)
		add(c.Modified)
		add(c.Removed)
	}
	msg := ""
	if p.HeadCommit != nil {
		if i := strings.IndexByte(p.HeadCommit.Message, '\n'); i >= 0 {
			msg = strings.TrimSpace(p.HeadCommit.Message[:i])
		} else {
			msg = strings.TrimSpace(p.HeadCommit.Message)
		}
	}
	return Push{
		Repo:    p.Repository.FullName,
		Branch:  branch,
		Commit:  p.After,
		Message: msg,
		Paths:   paths,
	}, true
}

type PullRequest struct {
	Action string
	Repo   string
	Branch string
	Number int
}

// ParsePullRequest reads a pull_request event; pull requests from forks are ignored since their branches live outside the repo.
func ParsePullRequest(body []byte) (PullRequest, bool) {
	var p struct {
		Action      string `json:"action"`
		Number      int    `json:"number"`
		PullRequest struct {
			Head struct {
				Ref  string `json:"ref"`
				Repo *struct {
					FullName string `json:"full_name"`
				} `json:"repo"`
			} `json:"head"`
		} `json:"pull_request"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	if json.Unmarshal(body, &p) != nil || p.Repository.FullName == "" || p.PullRequest.Head.Ref == "" {
		return PullRequest{}, false
	}
	if head := p.PullRequest.Head.Repo; head == nil || head.FullName != p.Repository.FullName {
		return PullRequest{}, false
	}
	return PullRequest{Action: p.Action, Repo: p.Repository.FullName, Branch: p.PullRequest.Head.Ref, Number: p.Number}, true
}

// apiBase is GitHub's REST root; tests point it at a fake.
var apiBase = "https://api.github.com"

// ErrForbidden means the app isn't allowed to do this, usually a permission it was never granted.
var ErrForbidden = errors.New("github app lacks permission")

var errNotFound = errors.New("not found on github")

func (c *Config) call(method, endpoint string, body, out any) error {
	token, err := c.InstallationToken()
	if err != nil {
		return err
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, endpoint, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusForbidden:
		return ErrForbidden
	case resp.StatusCode == http.StatusNotFound:
		return errNotFound
	case resp.StatusCode >= 300:
		return fmt.Errorf("github returned %s", resp.Status)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// OpenPullRequest finds the open pull request whose head is branch in repo ("owner/name"); 0 when there is none.
func (c *Config) OpenPullRequest(repo, branch string) (int, error) {
	owner, _, _ := strings.Cut(repo, "/")
	var prs []struct {
		Number int `json:"number"`
	}
	u := fmt.Sprintf(apiBase+"/repos/%s/pulls?state=open&head=%s", repo, url.QueryEscape(owner+":"+branch))
	if err := c.call(http.MethodGet, u, nil, &prs); err != nil || len(prs) == 0 {
		return 0, err
	}
	return prs[0].Number, nil
}

// SetStatus sets a commit status; state is pending, success, failure or error.
func (c *Config) SetStatus(repo, sha, state, context, description, targetURL string) error {
	if len(description) > 140 {
		description = description[:140]
	}
	return c.call(http.MethodPost, fmt.Sprintf(apiBase+"/repos/%s/statuses/%s", repo, sha), map[string]string{
		"state": state, "context": context, "description": description, "target_url": targetURL,
	}, nil)
}

// UpsertComment edits the comment if it still exists, otherwise posts a new one, returning the comment's id.
func (c *Config) UpsertComment(repo string, pr int, commentID int64, body string) (int64, error) {
	msg := map[string]string{"body": body}
	if commentID != 0 {
		err := c.call(http.MethodPatch, fmt.Sprintf(apiBase+"/repos/%s/issues/comments/%d", repo, commentID), msg, nil)
		if !errors.Is(err, errNotFound) {
			return commentID, err
		}
	}
	var out struct {
		ID int64 `json:"id"`
	}
	err := c.call(http.MethodPost, fmt.Sprintf(apiBase+"/repos/%s/issues/%d/comments", repo, pr), msg, &out)
	return out.ID, err
}
