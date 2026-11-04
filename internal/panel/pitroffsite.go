package panel

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nuelScript/skiff/internal/docker"
	"github.com/nuelScript/skiff/internal/s3"
)

const (
	pitrOffsiteEvery = 30 * time.Second
	pitrOffsiteGrace = int64(10 * 60) // a bucket that fails for less than this isn't worth an alert
)

// pitrCopying keeps it to one pass of each kind per database, so a slow snapshot upload doesn't hold up the segments.
var pitrCopying sync.Map

func historyRoot(prefix string, d dbRow) string {
	root := fmt.Sprintf("%s-%s/history/", d.Name, d.ID)
	if p := strings.Trim(prefix, "/"); p != "" {
		root = p + "/" + root
	}
	return root
}

func newHistoryGen() string {
	return time.Now().UTC().Format("20060102T150405Z") + "-" + randToken()[:6]
}

// pitrGen names the current run of history. A run with a gap or different data can't be replayed on top of the last one, so each gets its own folder in the bucket.
func pitrGen(id string) string {
	var gen string
	_ = sqlDB.QueryRow(`SELECT gen FROM pitr WHERE db_id=?`, id).Scan(&gen)
	if gen == "" {
		gen = startHistoryGen(id)
	}
	return gen
}

func startHistoryGen(id string) string {
	gen := newHistoryGen()
	_, _ = sqlDB.Exec(`UPDATE pitr SET gen=?, offsite_at=0, offsite_error='' WHERE db_id=?`, gen, id)
	_, _ = sqlDB.Exec(`DELETE FROM pitr_offsite WHERE db_id=?`, id)
	return gen
}

// forgetHistoryCopies makes the next pass copy everything kept locally, for when a team's bucket changes.
func forgetHistoryCopies(team string) {
	_, _ = sqlDB.Exec(`DELETE FROM pitr_offsite WHERE db_id IN (SELECT id FROM databases WHERE team=?)`, team)
	_, _ = sqlDB.Exec(`UPDATE pitr_bases SET offsite=0 WHERE db_id IN (SELECT id FROM databases WHERE team=?)`, team)
	_, _ = sqlDB.Exec(`UPDATE pitr SET offsite_at=0, offsite_error='' WHERE db_id IN (SELECT id FROM databases WHERE team=?)`, team)
}

func (p *Panel) pitrOffsiteLoop() {
	time.Sleep(pitrOffsiteEvery)
	for {
		guard("pitrOffsiteLoop", p.pitrOffsiteTick)
		time.Sleep(pitrOffsiteEvery)
	}
}

func (p *Panel) pitrOffsiteTick() {
	rows, err := sqlDB.Query(`SELECT db_id FROM pitr WHERE paused=''`)
	if err != nil {
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		d, ok := getDB(id)
		if !ok || p.eng.State(d.Container) != "running" {
			continue
		}
		t := getOffsite(d.Team)
		if !t.configured() {
			continue
		}
		pitrOnce("segments:"+id, func() { p.copySegments(d, t) })
		pitrOnce("bases:"+id, func() { p.copyBases(d, t) })
	}
}

func pitrOnce(key string, fn func()) {
	if _, busy := pitrCopying.LoadOrStore(key, true); busy {
		return
	}
	go func() {
		defer pitrCopying.Delete(key)
		guard("pitrOffsite", fn)
	}()
}

// copySegments records when a pass began, because everything archived by then is in the bucket once it succeeds.
func (p *Panel) copySegments(d dbRow, t OffsiteTarget) {
	began := time.Now().Unix()
	gen := pitrGen(d.ID)
	if err := p.uploadSegments(d, t, gen); err != nil {
		log.Printf("skiff: copying history of %s offsite: %v", d.Name, err)
		noteHistoryCopyError(d, err)
		return
	}
	_, _ = sqlDB.Exec(`UPDATE pitr SET offsite_at=?, offsite_error='' WHERE db_id=? AND gen=?`, began, d.ID, gen)
}

// maxSegmentBytes bounds a file read from the database's archive directory, which the database's own user can write to; WAL segments are 16 MiB by default.
const maxSegmentBytes = 80 << 20

type capWriter struct {
	buf bytes.Buffer
	max int
}

func (w *capWriter) Write(b []byte) (int, error) {
	if w.buf.Len()+len(b) > w.max {
		return 0, errors.New("file is larger than a WAL segment should be")
	}
	return w.buf.Write(b)
}

// readSegment reads one archived file, refusing it by size before reading and again while reading.
func readSegment(run func(cmd []string, out io.Writer) error, name string) ([]byte, error) {
	path := pitrMount + "/" + name
	var sz strings.Builder
	if err := run([]string{"stat", "-c", "%s", path}, &sz); err != nil {
		return nil, err
	}
	n, err := strconv.ParseInt(strings.TrimSpace(sz.String()), 10, 64)
	if err != nil || n < 0 || n > maxSegmentBytes {
		return nil, fmt.Errorf("unexpected size %q", strings.TrimSpace(sz.String()))
	}
	w := &capWriter{max: maxSegmentBytes}
	if err := run([]string{"cat", path}, w); err != nil {
		return nil, err
	}
	return w.buf.Bytes(), nil
}

func (p *Panel) uploadSegments(d dbRow, t OffsiteTarget, gen string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	var out strings.Builder
	if err := p.eng.Exec(ctx, d.Container, []string{"ls", pitrMount}, nil, &out); err != nil {
		return err
	}
	done := map[string]bool{}
	rows, err := sqlDB.Query(`SELECT name FROM pitr_offsite WHERE db_id=? AND gen=?`, d.ID, gen)
	if err != nil {
		return err
	}
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil {
			done[name] = true
		}
	}
	rows.Close()
	root := historyRoot(t.Prefix, d) + gen + "/wal/"
	c := t.client()
	var skipped error
	for _, name := range strings.Fields(out.String()) {
		if !strings.HasSuffix(name, ".gz") || done[name] {
			continue
		}
		run := func(cmd []string, w io.Writer) error { return p.eng.Exec(ctx, d.Container, cmd, nil, w) }
		body, err := readSegment(run, name)
		if err != nil {
			log.Printf("skiff: skipping %s of %s: %v", name, d.Name, err)
			if skipped == nil {
				skipped = fmt.Errorf("couldn't copy %s: %w", name, err)
			}
			continue
		}
		if err := c.Put(ctx, root+name, bytes.NewReader(body), int64(len(body))); err != nil {
			return err
		}
		if _, err := sqlDB.Exec(`INSERT OR IGNORE INTO pitr_offsite(db_id,gen,name) VALUES(?,?,?)`, d.ID, gen, name); err != nil {
			return err
		}
	}
	return skipped
}

// copyBases only sends a snapshot once a later pass has copied its segments, so the newest snapshot in the bucket can always be replayed.
func (p *Panel) copyBases(d dbRow, t OffsiteTarget) {
	var gen string
	var copiedAt int64
	if sqlDB.QueryRow(`SELECT gen,offsite_at FROM pitr WHERE db_id=?`, d.ID).Scan(&gen, &copiedAt) != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	c := t.client()
	sent := false
	for _, b := range listPITRBases(d.ID) {
		if b.Offsite || b.Gen != gen || b.Created >= copiedAt {
			continue
		}
		if err := uploadBase(ctx, c, historyRoot(t.Prefix, d)+gen+"/base/", b); err != nil {
			log.Printf("skiff: copying a snapshot of %s offsite: %v", d.Name, err)
			noteHistoryCopyError(d, err)
			return
		}
		_, _ = sqlDB.Exec(`UPDATE pitr_bases SET offsite=1 WHERE id=?`, b.ID)
		sent = true
	}
	if sent {
		if err := pruneHistoryCopies(ctx, c, historyRoot(t.Prefix, d), gen, d.ID); err != nil {
			log.Printf("skiff: pruning offsite history of %s: %v", d.Name, err)
		}
	}
}

func uploadBase(ctx context.Context, c *s3.Client, root string, b pitrBase) error {
	f, err := os.Open(pitrBasePath(b))
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	return c.Put(ctx, root+baseObjectName(b.Created, b.WAL), f, info.Size())
}

func baseObjectName(created int64, wal string) string {
	return fmt.Sprintf("%d-%s.tar.gz", created, wal)
}

// parseBaseObject reads back the snapshot's time and the history file it starts from. The bucket has to be enough on its own, because the panel's records may be older than the bucket after a server is lost.
func parseBaseObject(name string) (int64, string, bool) {
	stamp, wal, ok := strings.Cut(strings.TrimSuffix(name, ".tar.gz"), "-")
	if !ok {
		return 0, "", false
	}
	created, err := strconv.ParseInt(stamp, 10, 64)
	return created, wal, err == nil
}

// segmentNeeded says whether replay from a snapshot can read this file. Timeline files always count, and a snapshot of unknown start needs everything.
func segmentNeeded(name, start string) bool {
	if start == "" || !walFileRE.MatchString(segmentOf(name)) || !walFileRE.MatchString(segmentOf(start)) {
		return true
	}
	return segmentOf(name) >= segmentOf(start)
}

func segmentOf(name string) string {
	if len(name) < 24 {
		return name
	}
	return name[:24]
}

func noteHistoryCopyError(d dbRow, err error) {
	var was string
	var copiedAt int64
	if sqlDB.QueryRow(`SELECT offsite_error,offsite_at FROM pitr WHERE db_id=?`, d.ID).Scan(&was, &copiedAt) != nil {
		return
	}
	if time.Now().Unix()-copiedAt < pitrOffsiteGrace {
		return
	}
	_, _ = sqlDB.Exec(`UPDATE pitr SET offsite_error=? WHERE db_id=?`, trimCheckDetail(err.Error()), d.ID)
	if was == "" {
		go dispatchAlert(alertEvent{Team: d.Team, Kind: "database.recovery_failed", App: d.Name,
			Title:  "Point-in-time history of " + d.Name + " isn't reaching your offsite bucket",
			Detail: "Skiff will keep trying, and recovery on this server still works. Open Backups on it in the dashboard to see why."})
	}
}

func listObjects(ctx context.Context, c *s3.Client, prefix string) ([]s3.Object, []string, error) {
	var objects []s3.Object
	var folders []string
	token := ""
	for {
		l, err := c.List(ctx, prefix, token, 1000)
		if err != nil {
			return nil, nil, err
		}
		objects = append(objects, l.Objects...)
		folders = append(folders, l.Folders...)
		if l.NextToken == "" {
			return objects, folders, nil
		}
		token = l.NextToken
	}
}

// pruneHistoryCopies keeps as much in the bucket as on the server. Older runs go only now, because the current one has just got a snapshot and can stand in for them.
func pruneHistoryCopies(ctx context.Context, c *s3.Client, root, gen, dbID string) error {
	_, gens, err := listObjects(ctx, c, root)
	if err != nil {
		return err
	}
	for _, folder := range gens {
		if path.Base(folder) != gen {
			if err := deleteHistoryFolder(ctx, c, folder); err != nil {
				return err
			}
		}
	}
	bases, _, err := listObjects(ctx, c, root+gen+"/base/")
	if err != nil {
		return err
	}
	sortNewestBaseFirst(bases)
	if len(bases) > pitrKeepBases {
		for _, o := range bases[pitrKeepBases:] {
			if err := c.Delete(ctx, o.Key); err != nil {
				return err
			}
		}
		bases = bases[:pitrKeepBases]
	}
	if len(bases) == 0 {
		return nil
	}
	_, start, ok := parseBaseObject(path.Base(bases[len(bases)-1].Key))
	if !ok || start == "" {
		return nil
	}
	segments, _, err := listObjects(ctx, c, root+gen+"/wal/")
	if err != nil {
		return err
	}
	for _, o := range segments {
		if name := path.Base(o.Key); !segmentNeeded(name, start) {
			if err := c.Delete(ctx, o.Key); err != nil {
				return err
			}
			_, _ = sqlDB.Exec(`DELETE FROM pitr_offsite WHERE db_id=? AND gen=? AND name=?`, dbID, gen, name)
		}
	}
	return nil
}

func sortNewestBaseFirst(bases []s3.Object) {
	stamp := func(o s3.Object) int64 {
		created, _, _ := parseBaseObject(path.Base(o.Key))
		return created
	}
	sort.Slice(bases, func(i, j int) bool { return stamp(bases[i]) > stamp(bases[j]) })
}

func deleteHistoryFolder(ctx context.Context, c *s3.Client, folder string) error {
	for _, sub := range []string{"base/", "wal/"} {
		objects, _, err := listObjects(ctx, c, folder+sub)
		if err != nil {
			return err
		}
		for _, o := range objects {
			if err := c.Delete(ctx, o.Key); err != nil {
				return err
			}
		}
	}
	return nil
}

// removeHistoryCopies empties the database's history from the bucket, for when recovery is turned off.
func removeHistoryCopies(d dbRow) {
	t := getOffsite(d.Team)
	if !t.configured() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	c := t.client()
	root := historyRoot(t.Prefix, d)
	_, gens, err := listObjects(ctx, c, root)
	if err == nil {
		for _, folder := range gens {
			if err = deleteHistoryFolder(ctx, c, folder); err != nil {
				break
			}
		}
	}
	if err != nil {
		log.Printf("skiff: removing offsite history of %s: %v", d.Name, err)
	}
}

type historyPlan struct {
	Base     s3.Object
	Segments []s3.Object
	Ends     time.Time
}

// findHistory picks the newest snapshot of the newest run that has one, with the files replay needs.
func findHistory(ctx context.Context, c *s3.Client, root string) (historyPlan, bool, error) {
	_, gens, err := listObjects(ctx, c, root)
	if err != nil {
		return historyPlan{}, false, err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(gens)))
	for _, folder := range gens {
		bases, _, err := listObjects(ctx, c, folder+"base/")
		if err != nil {
			return historyPlan{}, false, err
		}
		if len(bases) == 0 {
			continue
		}
		sortNewestBaseFirst(bases)
		plan := historyPlan{Base: bases[0], Ends: bases[0].Modified}
		_, start, _ := parseBaseObject(path.Base(plan.Base.Key))
		segments, _, err := listObjects(ctx, c, folder+"wal/")
		if err != nil {
			return historyPlan{}, false, err
		}
		for _, o := range segments {
			if segmentNeeded(path.Base(o.Key), start) {
				plan.Segments = append(plan.Segments, o)
				if o.Modified.After(plan.Ends) {
					plan.Ends = o.Modified
				}
			}
		}
		return plan, true, nil
	}
	return historyPlan{}, false, nil
}

// recoverFromHistory rebuilds a lost database from the bucket, up to the last history that reached it. The data is proven to start before the database comes back, and false means the caller should fall back to the daily backup.
func (p *Panel) recoverFromHistory(d dbRow) (bool, error) {
	if d.Engine != "postgres" {
		return false, nil
	}
	t := getOffsite(d.Team)
	if !t.configured() {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	c := t.client()
	plan, ok, err := findHistory(ctx, c, historyRoot(t.Prefix, d))
	if err != nil || !ok {
		return false, err
	}
	log.Printf("skiff: offsite history of %s: snapshot %s, %d files, ends %s", d.Name, plan.Base.Key, len(plan.Segments), plan.Ends.UTC().Format(time.RFC3339))
	var newestDump int64
	_ = sqlDB.QueryRow(`SELECT MAX(created) FROM offsite_objects WHERE db_id=?`, d.ID).Scan(&newestDump)
	if newestDump > plan.Ends.Unix() {
		log.Printf("skiff: a backup of %s is newer than its offsite history, so using that", d.Name)
		return false, nil
	}
	if err := os.MkdirAll(backupsDir(), 0o700); err != nil {
		return false, err
	}
	if err := hasRoomFor(backupsDir(), plan.Base.Size*5); err != nil {
		return false, err
	}
	if err := p.fetchSegments(ctx, c, d, plan.Segments); err != nil {
		return false, fmt.Errorf("downloading its history: %w", err)
	}
	base, err := c.Get(ctx, plan.Base.Key)
	if err != nil {
		return false, err
	}
	defer base.Close()
	if err := p.recoverIntoVolume(ctx, d, d.volume(), base, ""); err != nil {
		_ = p.eng.RemoveVolume(d.volume())
		return false, err
	}
	if !pitrEnabled(d.ID) {
		_ = p.eng.RemoveVolume(pitrVolume(d))
		return true, nil
	}
	if err := p.inWALVolume(ctx, d, "find "+pitrMount+" -type f ! -name '*.history*' -delete"); err != nil {
		log.Printf("skiff: clearing history of %s: %v", d.Name, err)
	}
	for _, b := range listPITRBases(d.ID) {
		deletePITRBase(b)
	}
	startHistoryGen(d.ID)
	return true, nil
}

// fetchSegments streams the files straight into the volume, so a long history needs one container and no temporary copy.
func (p *Panel) fetchSegments(ctx context.Context, c *s3.Client, d dbRow, segments []s3.Object) error {
	pr, pw := io.Pipe()
	defer pr.Close()
	wrote := make(chan error, 1)
	go func() {
		err := writeSegmentsTar(ctx, c, pw, segments)
		_ = pw.CloseWithError(err)
		wrote <- err
	}()
	script := "set -e; cd " + pitrMount + "; find . -type f -delete; tar xf -; chown -R postgres:postgres ."
	err := p.eng.RunIO(ctx, d.image(), []docker.Mount{{Name: pitrVolume(d), Destination: pitrMount}}, script, pr, &bytes.Buffer{})
	_ = pr.CloseWithError(errors.New("stopped reading"))
	if werr := <-wrote; werr != nil {
		return werr
	}
	return err
}

func writeSegmentsTar(ctx context.Context, c *s3.Client, w io.Writer, segments []s3.Object) error {
	tw := tar.NewWriter(w)
	for _, o := range segments {
		rc, err := c.Get(ctx, o.Key)
		if err != nil {
			return err
		}
		err = tw.WriteHeader(&tar.Header{Name: path.Base(o.Key), Mode: 0o600, Size: o.Size, Typeflag: tar.TypeReg})
		if err == nil {
			_, err = io.Copy(tw, rc)
		}
		rc.Close()
		if err != nil {
			return err
		}
	}
	return tw.Close()
}
