package panel

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nuelScript/skiff/internal/docker"
	"github.com/nuelScript/skiff/internal/registry"
)

const (
	appLogKeep   = 7 * 24 * time.Hour
	appLogDayCap = 50 << 20
	appLogFirst  = 24 * time.Hour // how far back to start for an app with nothing saved yet
)

func appLogDir(app string) string { return filepath.Join(skiffDir(), "logs", sanitizeName(app)) }

type logWriter struct {
	mu   sync.Mutex
	day  string
	f    *os.File
	size int64
	full bool
	last time.Time
}

type logCollector struct {
	mu      sync.Mutex
	writers map[string]*logWriter
	follows map[string]context.CancelFunc // container → stop
}

var appLogs = &logCollector{writers: map[string]*logWriter{}, follows: map[string]context.CancelFunc{}}

func (c *logCollector) writer(app string) *logWriter {
	c.mu.Lock()
	defer c.mu.Unlock()
	w := c.writers[app]
	if w == nil {
		w = &logWriter{last: lastLoggedAt(app)}
		c.writers[app] = w
	}
	return w
}

// forget deletes an app's saved logs along with the app; the name can be taken again, by another team too.
func (c *logCollector) forget(app string) {
	c.mu.Lock()
	w := c.writers[app]
	delete(c.writers, app)
	c.mu.Unlock()
	if w != nil {
		w.mu.Lock()
		if w.f != nil {
			w.f.Close()
		}
		w.f, w.full = nil, true
		w.mu.Unlock()
	}
	_ = os.RemoveAll(appLogDir(app))
}

// lastLoggedAt reads the newest saved line's time so a restart resumes where collection left off.
func lastLoggedAt(app string) time.Time {
	days, _ := filepath.Glob(filepath.Join(appLogDir(app), "*.log"))
	sort.Strings(days)
	for i := len(days) - 1; i >= 0; i-- {
		f, err := os.Open(days[i])
		if err != nil {
			continue
		}
		var last string
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			last = sc.Text()
		}
		f.Close()
		if stamp, _, ok := strings.Cut(last, "\t"); ok {
			if t, err := time.Parse(time.RFC3339Nano, stamp); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}

func (w *logWriter) write(app, container string, ts time.Time, stream, text string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	day := ts.UTC().Format("2006-01-02")
	if day != w.day {
		if w.f != nil {
			w.f.Close()
		}
		w.f, w.day, w.full = nil, day, false
		if err := os.MkdirAll(appLogDir(app), 0o700); err != nil {
			return
		}
		f, err := os.OpenFile(filepath.Join(appLogDir(app), day+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		info, _ := f.Stat()
		w.f, w.size = f, info.Size()
	}
	if w.f == nil || w.full {
		return
	}
	if w.size >= appLogDayCap {
		w.full = true
		fmt.Fprintf(w.f, "%s\tskiff\terr\tlog limit of %d MB reached for today; later lines aren't kept\n", time.Now().UTC().Format(time.RFC3339Nano), appLogDayCap>>20)
		return
	}
	line := ts.UTC().Format(time.RFC3339Nano) + "\t" + container + "\t" + stream + "\t" + strings.ReplaceAll(text, "\t", "    ") + "\n"
	n, _ := w.f.WriteString(line)
	w.size += int64(n)
	if ts.After(w.last) {
		w.last = ts
	}
}

// sync follows every running app and worker container that isn't followed yet; a follower ends when its container stops.
func (c *logCollector) sync(p *Panel) {
	targets, err := p.eng.AppContainerKeys()
	if err != nil {
		return
	}
	if workers, err := p.eng.WorkerContainerKeys(); err == nil {
		for k, v := range workers {
			targets[k] = v
		}
	}
	for container, app := range targets {
		c.mu.Lock()
		_, following := c.follows[container]
		if following {
			c.mu.Unlock()
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		c.follows[container] = cancel
		c.mu.Unlock()

		w := c.writer(app)
		w.mu.Lock()
		since := w.last
		w.mu.Unlock()
		if since.IsZero() || time.Since(since) > appLogFirst {
			since = time.Now().Add(-appLogFirst)
		} else {
			since = since.Add(time.Nanosecond)
		}
		go func(container, app string) {
			defer func() {
				c.mu.Lock()
				delete(c.follows, container)
				c.mu.Unlock()
				cancel()
			}()
			_ = p.eng.FollowLogs(ctx, container, since, func(ts time.Time, stream, text string) {
				w.write(app, container, ts, stream, text)
			})
		}(container, app)
	}
}

func pruneAppLogs(now time.Time) {
	cutoff := now.Add(-appLogKeep).UTC().Format("2006-01-02")
	dirs, _ := os.ReadDir(filepath.Join(skiffDir(), "logs"))
	for _, d := range dirs {
		files, _ := filepath.Glob(filepath.Join(skiffDir(), "logs", d.Name(), "*.log"))
		for _, f := range files {
			if strings.TrimSuffix(filepath.Base(f), ".log") < cutoff {
				_ = os.Remove(f)
			}
		}
	}
}

func (p *Panel) appLogLoop() {
	var pruned time.Time
	for {
		guard("appLogs", func() {
			appLogs.sync(p)
			if time.Since(pruned) > time.Hour {
				pruneAppLogs(time.Now())
				pruned = time.Now()
			}
		})
		time.Sleep(10 * time.Second)
	}
}

type logLine struct {
	T         string `json:"t"`
	Container string `json:"container"`
	Stream    string `json:"stream"`
	Text      string `json:"text"`
}

var errorish = regexp.MustCompile(`(?i)\b(error|exception|panic|fatal|traceback|unhandled|critical|failed)\b`)

// searchAppLogs returns the newest matching lines in [since, until), oldest first, and whether older matches were cut off by limit.
func searchAppLogs(app, q string, errorsOnly bool, since, until time.Time, limit int) ([]logLine, bool) {
	days, _ := filepath.Glob(filepath.Join(appLogDir(app), "*.log"))
	sort.Sort(sort.Reverse(sort.StringSlice(days)))
	lq := strings.ToLower(q)
	var out []logLine
	for _, path := range days {
		day := strings.TrimSuffix(filepath.Base(path), ".log")
		if day < since.UTC().Format("2006-01-02") {
			break
		}
		if day > until.UTC().Format("2006-01-02") {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		var lines []logLine
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			parts := strings.SplitN(sc.Text(), "\t", 4)
			if len(parts) != 4 {
				continue
			}
			ts, err := time.Parse(time.RFC3339Nano, parts[0])
			if err != nil || ts.Before(since) || !ts.Before(until) {
				continue
			}
			text := parts[3]
			if lq != "" && !strings.Contains(strings.ToLower(text), lq) {
				continue
			}
			if errorsOnly && !errorish.MatchString(text) {
				continue
			}
			lines = append(lines, logLine{T: parts[0], Container: parts[1], Stream: parts[2], Text: text})
		}
		f.Close()
		// Newest-first across days: prepend this (older) day's lines.
		out = append(lines, out...)
		if len(out) > limit {
			return out[len(out)-limit:], true
		}
	}
	return out, false
}

func (p *Panel) handleLogHistory(w http.ResponseWriter, r *http.Request) {
	app := sanitizeName(r.URL.Query().Get("app"))
	if !p.canAccess(r, app) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	q := r.URL.Query()
	until := time.Now().Add(time.Second)
	since := until.Add(-24 * time.Hour)
	if v, err := strconv.ParseInt(q.Get("since"), 10, 64); err == nil && v > 0 {
		since = time.Unix(v, 0)
	}
	if v, err := strconv.ParseInt(q.Get("until"), 10, 64); err == nil && v > 0 {
		until = time.Unix(v, 0)
	}
	limit := 1000
	if v, err := strconv.Atoi(q.Get("limit")); err == nil && v > 0 && v <= 5000 {
		limit = v
	}
	lines, more := searchAppLogs(app, strings.TrimSpace(q.Get("q")), q.Get("errors") == "1", since, until, limit)
	if lines == nil {
		lines = []logLine{}
	}
	writeJSON(w, map[string]any{"lines": lines, "truncated": more})
}

type logTarget struct{ container, label string }

func (t logTarget) prefix() string {
	if t.label == "" {
		return ""
	}
	return "[" + t.label + "] "
}

func workerNames(app string) []string {
	var names []string
	for _, w := range listWorkers(app) {
		names = append(names, w.Name)
	}
	return names
}

// workerOf finds which worker a container belongs to. The longest match wins, so a worker named job doesn't claim job-email's containers.
func workerOf(app, container string, names []string) string {
	best := ""
	for _, n := range names {
		if strings.HasPrefix(container, docker.WorkerPrefix(app, n)) && len(n) > len(best) {
			best = n
		}
	}
	return best
}

// logTargets lists everything an app runs. Lines are labelled only when there's more than one source.
func logTargets(name string, a registry.App, workers, names []string) []logTarget {
	web := []string{a.Container}
	if len(a.Replicas) > 0 {
		web = web[:0]
		for _, r := range a.Replicas {
			web = append(web, r.Container)
		}
	}
	var out []logTarget
	for i, c := range web {
		label := name
		if len(web) > 1 {
			label = fmt.Sprintf("%s %d", name, i+1)
		}
		out = append(out, logTarget{c, label})
	}
	counts := map[string]int{}
	for _, c := range workers {
		label := workerOf(name, c, names)
		if label == "" {
			label = c
		}
		counts[label]++
		if counts[label] > 1 {
			label = fmt.Sprintf("%s %d", label, counts[label])
		}
		out = append(out, logTarget{c, label})
	}
	if len(out) == 1 {
		out[0].label = ""
	}
	return out
}

// followLogs merges live output until ctx ends or every container has stopped.
func (p *Panel) followLogs(ctx context.Context, targets []logTarget, emit func(line string)) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, t := range targets {
		wg.Add(1)
		go func(t logTarget) {
			defer wg.Done()
			pr, pw := io.Pipe()
			go func() { _ = p.eng.StreamLogs(ctx, t.container, pw); _ = pw.Close() }()
			sc := bufio.NewScanner(pr)
			sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
			for sc.Scan() {
				mu.Lock()
				emit(t.prefix() + sc.Text())
				mu.Unlock()
			}
			_ = pr.Close()
		}(t)
	}
	wg.Wait()
}
