package panel

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nuelScript/skiff/internal/auth"
)

// fakeDrain records what it's sent and fails while failing is set.
type fakeDrain struct {
	mu      sync.Mutex
	got     []drainEntry
	headers []string
	failing bool
}

func (f *fakeDrain) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failing {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	var batch []drainEntry
	_ = json.NewDecoder(r.Body).Decode(&batch)
	f.got = append(f.got, batch...)
	f.headers = append(f.headers, r.Header.Get("X-Key"))
}

func (f *fakeDrain) messages() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.got))
	for _, e := range f.got {
		out = append(out, e.Message)
	}
	return out
}

func startFakeDrain(t *testing.T) (*fakeDrain, string) {
	t.Helper()
	f := &fakeDrain{}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	prev := drainClient
	drainClient = srv.Client
	t.Cleanup(func() { drainClient = prev })
	return f, srv.URL
}

func writeDay(t *testing.T, app, day string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(appLogDir(app), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(appLogDir(app), day+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, l := range lines {
		fmt.Fprintf(f, "%sT10:00:00Z\tc1\tout\t%s\n", day, l)
	}
}

func TestReadDrainBatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeDay(t, "api", "2026-11-01", "one", "two")
	writeDay(t, "api", "2026-11-02", "three")
	path := filepath.Join(appLogDir("api"), "2026-11-02.log")
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.WriteString("2026-11-02T10:00:00Z\tc1\tout\tstill being writ")
	f.Close()

	got, cur, err := readDrainBatch("api", drainCursor{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Message != "one" || got[2].Message != "three" || got[0].App != "api" || got[0].Container != "c1" {
		t.Fatalf("batch across days = %+v", got)
	}
	if cur.Day != "2026-11-02" {
		t.Fatalf("cursor = %+v", cur)
	}
	if again, _, _ := readDrainBatch("api", cur); len(again) != 0 {
		t.Fatalf("a partial line must wait for its newline: %+v", again)
	}
	f, _ = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.WriteString("ten\n")
	f.Close()
	if rest, _, _ := readDrainBatch("api", cur); len(rest) != 1 || rest[0].Message != "still being written" {
		t.Fatalf("after the line finished: %+v", rest)
	}

	// A cursor on a day that was pruned moves on to the oldest day left.
	if got, _, _ := readDrainBatch("api", drainCursor{Day: "2026-10-01", Pos: 999}); len(got) != 4 {
		t.Fatalf("from a pruned day: %d lines", len(got))
	}
}

func TestReadDrainBatchLimits(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	lines := make([]string, drainBatchLines+5)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}
	lines[0] = strings.Repeat("x", drainMessageBytes+50)
	writeDay(t, "api", "2026-11-01", lines...)
	got, cur, _ := readDrainBatch("api", drainCursor{})
	if len(got) != drainBatchLines || len(got[0].Message) != drainMessageBytes {
		t.Fatalf("batch = %d lines, first %d bytes", len(got), len(got[0].Message))
	}
	rest, _, _ := readDrainBatch("api", cur)
	if len(rest) != 5 || rest[4].Message != fmt.Sprintf("line %d", drainBatchLines+4) {
		t.Fatalf("remainder = %+v", rest)
	}
}

func TestShipLogs(t *testing.T) {
	openTestDB(t)
	t.Setenv("HOME", t.TempDir())
	fake, url := startFakeDrain(t)
	_ = putSource(Source{App: "api", Team: "t1", Port: "80"})
	_ = putSource(Source{App: "secret", Team: "t2", Port: "80"})
	writeDay(t, "api", "2026-11-01", "before the drain existed")
	writeDay(t, "secret", "2026-11-01", "another team's line")

	d := LogDrain{Team: "t1", URL: url, HeaderName: "X-Key", HeaderValue: "k"}
	if err := saveLogDrain(d); err != nil {
		t.Fatal(err)
	}
	if sent, err := shipLogs(d); err != nil || sent {
		t.Fatalf("history must not be sent: sent=%v err=%v %v", sent, err, fake.messages())
	}

	writeDay(t, "api", "2026-11-01", "first")
	// An app created after the drain starts from its beginning.
	_ = putSource(Source{App: "web", Team: "t1", Port: "80"})
	writeDay(t, "web", "2026-11-01", "web line")
	if _, err := shipLogs(d); err != nil {
		t.Fatal(err)
	}
	if got := fake.messages(); len(got) != 2 || got[0] != "first" || got[1] != "web line" {
		t.Fatalf("sent = %v", got)
	}
	if fake.headers[0] != "k" {
		t.Fatalf("header = %q", fake.headers[0])
	}

	// A refused batch stays put and goes out once the endpoint recovers, exactly once.
	fake.failing = true
	writeDay(t, "api", "2026-11-02", "during the outage")
	sent, err := shipLogs(d)
	if err == nil || sent || !strings.Contains(err.Error(), "503") {
		t.Fatalf("outage: sent=%v err=%v", sent, err)
	}
	noteDrainResult("t1", sent, err)
	if got, _ := getLogDrain("t1"); !strings.Contains(got.LastError, "503") {
		t.Fatalf("last error = %q", got.LastError)
	}
	fake.failing = false
	sent, err = shipLogs(d)
	noteDrainResult("t1", sent, err)
	if err != nil || !sent {
		t.Fatalf("after the outage: sent=%v err=%v", sent, err)
	}
	if got := fake.messages(); len(got) != 3 || got[2] != "during the outage" {
		t.Fatalf("sent = %v", got)
	}
	if got, _ := getLogDrain("t1"); got.LastError != "" || got.LastOK == 0 {
		t.Fatalf("status after recovery = %+v", got)
	}
	if sent, _ := shipLogs(d); sent {
		t.Fatal("nothing new should mean nothing sent")
	}
	for _, m := range fake.messages() {
		if strings.Contains(m, "another team") {
			t.Fatal("another team's logs were sent")
		}
	}

	// Moving to a new destination starts from now.
	writeDay(t, "api", "2026-11-02", "for the old destination")
	d.URL = url + "/new"
	_ = saveLogDrain(d)
	if sent, _ := shipLogs(d); sent {
		t.Fatal("a new destination received old lines")
	}

	deleteSource("web")
	if c := getDrainCursor("t1", "web"); c.Day != "" {
		t.Fatalf("deleting the project left its cursor: %+v", c)
	}
	deleteLogDrain("t1")
	if _, ok := getLogDrain("t1"); ok || getDrainCursor("t1", "api").Day != "" {
		t.Fatal("deleting the drain left rows behind")
	}
}

func TestDrainRefusesPrivateAddresses(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit = true }))
	defer srv.Close()
	prev := drainClient
	drainClient = newDrainClient
	defer func() { drainClient = prev }()
	err := postDrain(LogDrain{URL: srv.URL}, []drainEntry{{Message: "x"}})
	if err == nil || hit || !strings.Contains(err.Error(), "non-public") {
		t.Fatalf("loopback should be refused: hit=%v err=%v", hit, err)
	}
}

func TestDrainDoesNotFollowRedirects(t *testing.T) {
	t.Setenv("SKIFF_DRAIN_ALLOW_PRIVATE", "1")
	var leaked bool
	other := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { leaked = r.Header.Get("X-Key") != "" }))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	prev := drainClient
	drainClient = newDrainClient
	defer func() { drainClient = prev }()
	err := postDrain(LogDrain{URL: srv.URL, HeaderName: "X-Key", HeaderValue: "k"}, []drainEntry{{Message: "x"}})
	if err == nil || leaked {
		t.Fatalf("redirect: leaked=%v err=%v", leaked, err)
	}
}

func TestValidateDrain(t *testing.T) {
	good := []LogDrain{
		{URL: "https://logs.example.com/in"},
		{URL: "https://logs.example.com/in", HeaderName: "Authorization", HeaderValue: "Bearer x"},
		{URL: "https://http-intake.example.com/api/v2/logs?ddsource=skiff", HeaderName: "DD-API-KEY", HeaderValue: "x"},
	}
	for _, d := range good {
		if err := validateDrain(d); err != nil {
			t.Errorf("%+v: %v", d, err)
		}
	}
	bad := []LogDrain{
		{URL: ""},
		{URL: "logs.example.com"},
		{URL: "ftp://logs.example.com"},
		{URL: "http://logs.example.com/in"},
		{URL: "https://logs.example.com", HeaderName: "Authorization"},
		{URL: "https://logs.example.com", HeaderName: "Bad Name", HeaderValue: "x"},
		{URL: "https://logs.example.com", HeaderName: "Host", HeaderValue: "x"},
		{URL: "https://logs.example.com", HeaderName: "X-Key", HeaderValue: "a\r\nb"},
	}
	for _, d := range bad {
		if validateDrain(d) == nil {
			t.Errorf("%+v was accepted", d)
		}
	}
}

func TestLogDrainHandler(t *testing.T) {
	openTestDB(t)
	t.Setenv("HOME", t.TempDir())
	store := auth.NewStore(sqlDB)
	p := &Panel{auth: store, domain: "skiff.test"}
	owner, team, err := store.CreateUser("owner@acme.dev", "Owner", "password123")
	if err != nil {
		t.Fatal(err)
	}
	member, _, err := store.CreateUser("member@acme.dev", "Member", "password123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO memberships(user_id,team_id,role) VALUES(?,?,'member')`, member.ID, team.ID); err != nil {
		t.Fatal(err)
	}
	_ = putSession("owner-token", owner.ID, team.ID)
	_ = putSession("member-token", member.ID, team.ID)
	call := func(token, method, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/log-drain", strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: "skiff_session", Value: token})
		w := httptest.NewRecorder()
		p.handleLogDrain(w, req)
		return w
	}

	if w := call("member-token", http.MethodGet, ""); w.Code != http.StatusForbidden {
		t.Fatalf("member read: %d", w.Code)
	}
	if w := call("member-token", http.MethodPut, `{"url":"https://evil.example.com"}`); w.Code != http.StatusForbidden {
		t.Fatalf("member write: %d", w.Code)
	}
	if w := call("owner-token", http.MethodPut, `{"url":"http://logs.example.com"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("plain http: %d", w.Code)
	}
	if w := call("owner-token", http.MethodPut, `{"url":"https://logs.example.com/a","headerName":"X-Key","headerValue":"s3cret"}`); w.Code != http.StatusNoContent {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	if w := call("owner-token", http.MethodGet, ""); strings.Contains(w.Body.String(), "s3cret") || !strings.Contains(w.Body.String(), `"headerSet":true`) {
		t.Fatalf("view = %s", w.Body)
	}
	// A blank value keeps the saved one on the same host, but it never follows the drain to a new host.
	if w := call("owner-token", http.MethodPut, `{"url":"https://logs.example.com/b","headerName":"X-Key"}`); w.Code != http.StatusNoContent {
		t.Fatalf("same host: %d %s", w.Code, w.Body)
	}
	if d, _ := getLogDrain(team.ID); d.HeaderValue != "s3cret" || d.URL != "https://logs.example.com/b" {
		t.Fatalf("after same-host save: %+v", d)
	}
	if w := call("owner-token", http.MethodPut, `{"url":"https://evil.example.com/","headerName":"X-Key"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("new host without a value: %d", w.Code)
	}
	if w := call("owner-token", http.MethodDelete, ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", w.Code)
	}
	if _, ok := getLogDrain(team.ID); ok {
		t.Fatal("the drain survived deletion")
	}
}

func TestForgetAppLogs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	w := appLogs.writer("gone")
	now := time.Now()
	w.write("gone", "c", now, "out", "old team's line")
	appLogs.forget("gone")
	if lines, _ := searchAppLogs("gone", "", false, now.Add(-time.Hour), now.Add(time.Hour), 10); len(lines) != 0 {
		t.Fatalf("the name's next owner could read: %+v", lines)
	}
	if w.write("gone", "c", now, "out", "late line"); len(logDays("gone")) != 0 {
		t.Fatal("a late write brought the logs back")
	}
}
