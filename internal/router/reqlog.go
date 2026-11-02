package router

import (
	"bufio"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// The panel copies these into its database within seconds, so they only need to outlast a panel restart.
	reqLogKeepDays = 2
	// reqLogDayCap stops a very busy day filling the disk. Per-request detail past it is lost; the router's own totals stay exact.
	reqLogDayCap = 1 << 30
)

// ReqEntry is one proxied request, as appended to <dir>/<YYYY-MM-DD>.jsonl.
type ReqEntry struct {
	T  int64  `json:"t"` // unix millis
	A  string `json:"a"` // app
	M  string `json:"m"`
	P  string `json:"p"` // path, no query string
	S  int    `json:"s"`
	D  int64  `json:"d"`           // duration ms
	B  int64  `json:"b"`           // response bytes
	R  string `json:"r,omitempty"` // referring host, when it's another site
	V  string `json:"v"`           // visitor: keyed hash of ip+user agent, rotating daily
	Bt bool   `json:"bt,omitempty"`
}

// ReqLog appends request entries from a buffered channel so the proxy path never waits on disk; when the buffer is full, entries are dropped.
type ReqLog struct {
	dir string
	key []byte
	ch  chan ReqEntry
}

func NewReqLog(dir string) *ReqLog {
	if os.MkdirAll(dir, 0o700) != nil {
		return nil
	}
	return &ReqLog{dir: dir, key: visitorKey(filepath.Join(filepath.Dir(dir), "visitor.key")), ch: make(chan ReqEntry, 4096)}
}

func visitorKey(path string) []byte {
	if b, err := os.ReadFile(path); err == nil && len(b) >= 32 {
		return b
	}
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	tmp := path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		if os.Link(tmp, path) != nil {
			_ = os.Remove(tmp)
			if existing, err := os.ReadFile(path); err == nil && len(existing) >= 32 {
				return existing
			}
		}
		_ = os.Remove(tmp)
	}
	return b
}

func (l *ReqLog) Record(app string, r *http.Request, status int, bytes int64, dur time.Duration) {
	if l == nil || strings.HasPrefix(r.UserAgent(), "skiff-uptime/") {
		return
	}
	now := time.Now()
	ua := r.UserAgent()
	e := ReqEntry{
		T: now.UnixMilli(), A: app, M: r.Method, P: truncate(r.URL.Path, 256), S: status,
		D: dur.Milliseconds(), B: bytes, R: referrerHost(r), Bt: isBot(ua),
		V: l.visitor(clientIP(r), ua, now),
	}
	select {
	case l.ch <- e:
	default:
	}
}

func (l *ReqLog) visitor(ip, ua string, now time.Time) string {
	h := hmac.New(sha256.New, l.key)
	h.Write([]byte(now.UTC().Format("2006-01-02") + "|" + ip + "|" + ua))
	return hex.EncodeToString(h.Sum(nil)[:6])
}

func (l *ReqLog) Run() {
	var (
		f       *os.File
		w       *bufio.Writer
		day     string
		size    int64
		flush   = time.NewTicker(2 * time.Second)
		cleanup = time.NewTicker(time.Hour)
	)
	defer flush.Stop()
	defer cleanup.Stop()
	l.prune(time.Now())
	for {
		select {
		case e := <-l.ch:
			d := time.UnixMilli(e.T).UTC().Format("2006-01-02")
			if d != day || f == nil {
				if w != nil {
					_ = w.Flush()
					_ = f.Close()
				}
				var err error
				f, err = os.OpenFile(filepath.Join(l.dir, d+".jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
				if err != nil {
					f, w = nil, nil
					continue
				}
				w, day = bufio.NewWriterSize(f, 64*1024), d
				size = 0
				if fi, err := f.Stat(); err == nil {
					size = fi.Size()
				}
			}
			if size >= reqLogDayCap {
				continue
			}
			b, _ := json.Marshal(e)
			_, _ = w.Write(append(b, '\n'))
			size += int64(len(b)) + 1
		case <-flush.C:
			if w != nil {
				_ = w.Flush()
			}
		case <-cleanup.C:
			l.prune(time.Now())
		}
	}
}

func (l *ReqLog) prune(now time.Time) {
	cutoff := now.UTC().AddDate(0, 0, -reqLogKeepDays).Format("2006-01-02")
	entries, _ := os.ReadDir(l.dir)
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".jsonl") && strings.TrimSuffix(name, ".jsonl") < cutoff {
			_ = os.Remove(filepath.Join(l.dir, name))
		}
	}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func referrerHost(r *http.Request) string {
	ref := r.Referer()
	if ref == "" {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil || u.Host == "" || strings.EqualFold(hostOnly(u.Host), hostOnly(r.Host)) {
		return ""
	}
	return truncate(strings.ToLower(strings.TrimPrefix(u.Host, "www.")), 100)
}

var botMarkers = []string{"bot", "crawl", "spider", "slurp", "curl/", "wget/", "python-requests", "go-http-client", "scanner", "headless", "monitor", "httpclient", "zgrab", "masscan"}

func isBot(ua string) bool {
	if ua == "" {
		return true
	}
	ua = strings.ToLower(ua)
	for _, m := range botMarkers {
		if strings.Contains(ua, m) {
			return true
		}
	}
	return false
}
