package panel

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nuelScript/skiff/internal/config"
	"github.com/nuelScript/skiff/internal/docker"
	"github.com/nuelScript/skiff/internal/drain"
)

const (
	volumeBackupRetention = 3
	// Past this the app would be frozen for too long, so the copy is taken live and marked as such.
	volumePauseLimit  = 1 << 30
	volumeToolImage   = "alpine:3.20"
	volumeBackupLimit = 30 * time.Minute
)

type VolumeBackup struct {
	ID      string `json:"id"`
	Volume  string `json:"volume"`
	Size    int64  `json:"size"`
	Trigger string `json:"trigger"`
	Created int64  `json:"created"`
	Offsite string `json:"offsite,omitempty"`
	Live    bool   `json:"live,omitempty"`
}

type volumeBackupRow struct {
	ID, App, Volume, Team, File string
	Size                        int64
	Trigger                     string
	Created                     int64
	Offsite, OffsiteKey         string
	Live                        bool
}

const volumeBackupCols = `id,app,volume,team,file,size,trigger,created,offsite,offsite_key,live`

func scanVolumeBackup(row interface{ Scan(...any) error }) (volumeBackupRow, bool) {
	var b volumeBackupRow
	var live int
	if row.Scan(&b.ID, &b.App, &b.Volume, &b.Team, &b.File, &b.Size, &b.Trigger, &b.Created, &b.Offsite, &b.OffsiteKey, &live) != nil {
		return volumeBackupRow{}, false
	}
	b.Live = live != 0
	return b, true
}

func getVolumeBackup(id string) (volumeBackupRow, bool) {
	return scanVolumeBackup(sqlDB.QueryRow(`SELECT `+volumeBackupCols+` FROM volume_backups WHERE id=?`, id))
}

func listVolumeBackups(app, volume string) []volumeBackupRow {
	rows, err := sqlDB.Query(`SELECT `+volumeBackupCols+` FROM volume_backups WHERE app=? AND volume=? ORDER BY created DESC, id`, app, volume)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []volumeBackupRow
	for rows.Next() {
		if b, ok := scanVolumeBackup(rows); ok {
			out = append(out, b)
		}
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

func volumeBackupDirName(app, volume string) string { return "vol-" + app + "-" + volume }

// volumeOffsiteOwner keys a volume's offsite copies; ':' can't appear in an app or volume name, so two pairs never share one.
func volumeOffsiteOwner(app, volume string) string { return "vol:" + app + ":" + volume }

// adoptVolumeOffsiteRows moves copies recorded under the older joined name to the new owner, matching on the key (which has the app and volume as separate path parts) so another pair that shared the old name keeps its own.
func adoptVolumeOffsiteRows(owner, app, volume string) {
	_, _ = sqlDB.Exec(`UPDATE offsite_objects SET db_id=? WHERE db_id=? AND instr(key, ?) > 0`,
		owner, volumeBackupDirName(app, volume), "volumes/"+app+"/"+volume+"/")
}

func volumeBackupPath(b volumeBackupRow) string {
	return filepath.Join(backupsDir(), volumeBackupDirName(b.App, b.Volume), b.File)
}

func deleteVolumeBackup(b volumeBackupRow) {
	_ = os.Remove(volumeBackupPath(b))
	_, _ = sqlDB.Exec(`DELETE FROM volume_backups WHERE id=?`, b.ID)
}

// removeVolumeBackups drops the local copies of a volume that no longer exists. Offsite copies stay, under the bucket's own lifecycle.
func removeVolumeBackups(app, volume string) {
	for _, b := range listVolumeBackups(app, volume) {
		deleteVolumeBackup(b)
	}
	_ = os.Remove(filepath.Join(backupsDir(), volumeBackupDirName(app, volume)))
}

func removeAppVolumeBackups(app string) {
	rows, err := sqlDB.Query(`SELECT DISTINCT volume FROM volume_backups WHERE app=?`, app)
	if err != nil {
		return
	}
	var names []string
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			names = append(names, n)
		}
	}
	rows.Close()
	for _, n := range names {
		removeVolumeBackups(app, n)
	}
}

func appIsDeploying(app string) bool {
	var n int
	_ = sqlDB.QueryRow(`SELECT COUNT(*) FROM deploys WHERE app=? AND status='building'`, app).Scan(&n)
	return n > 0
}

// diskFree reports the bytes available where backups are kept.
func diskFree(path string) (free, total int64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	bsize := int64(st.Bsize)
	return int64(st.Bavail) * bsize, int64(st.Blocks) * bsize, nil
}

// roomFor assumes nothing compresses and keeps a reserve, so a backup can't be what fills the server.
func roomFor(size, free, total int64) error {
	reserve := min(total/20, 1<<30)
	if free-size < reserve {
		return fmt.Errorf("not enough disk space: this needs up to %d MB plus %d MB kept free, and only %d MB is free", size>>20, reserve>>20, free>>20)
	}
	return nil
}

func hasRoomFor(path string, size int64) error {
	free, total, err := diskFree(path)
	if err != nil {
		return err
	}
	return roomFor(size, free, total)
}

// runVolumeBackup archives a volume from a throwaway container that mounts it read-only.
func (p *Panel) runVolumeBackup(src Source, volume, trigger string) (volumeBackupRow, error) {
	name := config.VolumeName(src.App, volume)
	if !p.eng.VolumeExists(name) {
		return volumeBackupRow{}, errors.New("the volume doesn't exist yet: deploy the app first")
	}
	if appIsDeploying(src.App) {
		return volumeBackupRow{}, errors.New("a deploy is running: try again when it finishes")
	}
	dir := filepath.Join(backupsDir(), volumeBackupDirName(src.App, volume))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return volumeBackupRow{}, err
	}
	size := p.eng.VolumeSizes()[name]
	if err := hasRoomFor(dir, size); err != nil {
		return volumeBackupRow{}, err
	}
	id := randToken()[:12]
	path := filepath.Join(dir, id+".tar.gz")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return volumeBackupRow{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), volumeBackupLimit)
	defer cancel()

	live := size > volumePauseLimit
	var paused []string
	if !live {
		paused = p.eng.PauseUsing(name)
	}
	mount := []docker.Mount{{Name: name, Destination: "/data:ro"}}
	rerr := p.eng.RunIO(ctx, volumeToolImage, mount, "tar czf - -C /data .", nil, f)
	p.eng.Unpause(paused)
	cerr := f.Close()
	if rerr == nil {
		rerr = cerr
	}
	if rerr != nil {
		_ = os.Remove(path)
		return volumeBackupRow{}, rerr
	}
	var written int64
	if info, err := os.Stat(path); err == nil {
		written = info.Size()
	}
	b := volumeBackupRow{
		ID: id, App: src.App, Volume: volume, Team: src.Team, File: id + ".tar.gz",
		Size: written, Trigger: trigger, Created: time.Now().Unix(), Live: live,
	}
	if _, err := sqlDB.Exec(`INSERT INTO volume_backups(`+volumeBackupCols+`) VALUES(?,?,?,?,?,?,?,?,'','',?)`,
		b.ID, b.App, b.Volume, b.Team, b.File, b.Size, b.Trigger, b.Created, b2i(b.Live)); err != nil {
		_ = os.Remove(path)
		return volumeBackupRow{}, err
	}
	all := listVolumeBackups(src.App, volume)
	for i := volumeBackupRetention; i < len(all); i++ {
		deleteVolumeBackup(all[i])
	}
	if getOffsite(b.Team).configured() {
		b.Offsite = "pending"
		setVolumeBackupOffsite(b.ID, b.Offsite, "")
		go guard("uploadVolumeOffsite", func() {
			err := uploadVolumeOffsite(b)
			noteOffsiteCopy(b.Team, b.App+"/"+b.Volume, err)
			if err != nil {
				log.Printf("skiff: offsite copy of %s/%s backup %s failed: %v", b.App, b.Volume, b.ID, err)
			}
		})
	}
	return b, nil
}

func setVolumeBackupOffsite(id, status, key string) {
	_, _ = sqlDB.Exec(`UPDATE volume_backups SET offsite=?, offsite_key=? WHERE id=?`, status, key, id)
}

func uploadVolumeOffsite(b volumeBackupRow) error {
	t := getOffsite(b.Team)
	if !t.configured() {
		return nil
	}
	fail := func(err error) error {
		setVolumeBackupOffsite(b.ID, "failed", "")
		return err
	}
	f, err := os.Open(volumeBackupPath(b))
	if err != nil {
		return fail(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return fail(err)
	}
	stamp := time.Unix(b.Created, 0).UTC().Format("20060102T150405Z")
	key := fmt.Sprintf("volumes/%s/%s/%s-%s", b.App, b.Volume, stamp, b.File)
	if prefix := strings.Trim(t.Prefix, "/"); prefix != "" {
		key = prefix + "/" + key
	}
	c := t.client()
	if err := c.Put(context.Background(), key, f, info.Size()); err != nil {
		return fail(err)
	}
	setVolumeBackupOffsite(b.ID, "ok", key)
	owner := volumeOffsiteOwner(b.App, b.Volume)
	adoptVolumeOffsiteRows(owner, b.App, b.Volume)
	_, _ = sqlDB.Exec(`INSERT OR REPLACE INTO offsite_objects(db_id,key,created) VALUES(?,?,?)`, owner, key, b.Created)
	pruneOffsite(c, owner, offsiteRetention)
	return nil
}

// checkVolumeArchive reads the whole archive, so a damaged file is found before anything changes.
func checkVolumeArchive(r io.Reader) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		if _, err := tr.Next(); errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return err
		}
		if _, err := io.Copy(io.Discard, tr); err != nil {
			return err
		}
	}
}

// openVolumeBackup returns the local file, fetching it from the bucket first if this server no longer has it.
func openVolumeBackup(ctx context.Context, b volumeBackupRow) (*os.File, error) {
	path := volumeBackupPath(b)
	if f, err := os.Open(path); err == nil {
		return f, nil
	}
	if b.OffsiteKey == "" {
		return nil, errors.New("the backup file is missing")
	}
	t := getOffsite(b.Team)
	if !t.configured() {
		return nil, errors.New("the backup is offsite, but no bucket is configured")
	}
	rc, err := t.client().Get(ctx, b.OffsiteKey)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	tmp := path + ".part"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp)
	_, err = io.Copy(out, rc)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return nil, err
	}
	return os.Open(path)
}

// Unpacks beside the live files while the app keeps running, so only the swap below needs it stopped.
const volumeExtractScript = `set -e
rm -rf /data/.restore-new /data/.restore-old
mkdir /data/.restore-new
tar xzf - -C /data/.restore-new || { rm -rf /data/.restore-new; echo "couldn't unpack the backup" >&2; exit 1; }`

// find exits 0 even when the mv it runs fails, so each step checks what's left behind instead.
const volumeSwapScript = `cd /data || exit 1
put_back() { find .restore-old -mindepth 1 -maxdepth 1 -exec mv {} . \; ; rmdir .restore-old; rm -rf .restore-new; echo "$1" >&2; exit 1; }
mkdir .restore-old || exit 1
find . -mindepth 1 -maxdepth 1 ! -name .restore-new ! -name .restore-old -exec mv {} .restore-old/ \;
[ -z "$(find . -mindepth 1 -maxdepth 1 ! -name .restore-new ! -name .restore-old)" ] || put_back "couldn't move the current files aside"
find .restore-new -mindepth 1 -maxdepth 1 -exec mv {} . \;
if ! rmdir .restore-new 2>/dev/null; then
  find . -mindepth 1 -maxdepth 1 ! -name .restore-new ! -name .restore-old -exec rm -rf {} +
  put_back "couldn't move the restored files in"
fi
rm -rf .restore-old`

func (p *Panel) restoreVolume(ctx context.Context, b volumeBackupRow, archive *os.File) error {
	name := config.VolumeName(b.App, b.Volume)
	mount := []docker.Mount{{Name: name, Destination: "/data"}}
	if err := p.eng.RunIO(ctx, volumeToolImage, mount, volumeExtractScript, archive, io.Discard); err != nil {
		return err
	}
	running := p.eng.RunningUsing(name)
	replaced := drain.Active(time.Now())
	for _, c := range running {
		if err := p.eng.Stop(c); err != nil {
			log.Printf("skiff: stopping %s for a restore: %v", c, err)
		}
	}
	swapErr := p.eng.RunIO(ctx, volumeToolImage, mount, volumeSwapScript, nil, io.Discard)
	for _, c := range running {
		if replaced[c] {
			continue // on its way out, and the router will remove it
		}
		if err := p.eng.Start(c); err != nil && swapErr == nil {
			swapErr = fmt.Errorf("starting %s again: %w", c, err)
		}
	}
	return swapErr
}

func toVolumeBackup(b volumeBackupRow) VolumeBackup {
	return VolumeBackup{ID: b.ID, Volume: b.Volume, Size: b.Size, Trigger: b.Trigger, Created: b.Created, Offsite: b.Offsite, Live: b.Live}
}

func (p *Panel) handleVolumeBackups(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodPost:
		app := sanitizeName(r.URL.Query().Get("app"))
		src, ok := getSource(app)
		if !ok || !p.canAccess(r, app) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		volume := r.URL.Query().Get("volume")
		known := false
		for _, v := range p.volumeDefs(src) {
			known = known || v.Name == volume
		}
		if !known {
			http.Error(w, "no such volume", http.StatusNotFound)
			return
		}
		if r.Method == http.MethodGet {
			out := []VolumeBackup{}
			for _, b := range listVolumeBackups(app, volume) {
				out = append(out, toVolumeBackup(b))
			}
			writeJSON(w, out)
			return
		}
		if src.Parent != "" || isPreviewStack(src.Stack) {
			http.Error(w, "previews use empty copies of a project's volumes, so they aren't backed up", http.StatusBadRequest)
			return
		}
		b, err := p.runVolumeBackup(src, volume, "manual")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		p.audit(r, "volume.backup", app, volume)
		writeJSON(w, toVolumeBackup(b))

	case http.MethodDelete:
		b, ok := getVolumeBackup(sanitizeID(r.URL.Query().Get("id")))
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if !p.canAccess(r, b.App) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		deleteVolumeBackup(b)
		p.audit(r, "volume.backup-delete", b.App, b.Volume)
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (p *Panel) handleVolumeBackupRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b, ok := getVolumeBackup(sanitizeID(r.URL.Query().Get("id")))
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	src, ok := getSource(b.App)
	if !ok || !p.canAccess(r, b.App) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if appIsDeploying(b.App) {
		http.Error(w, "a deploy is running: try again when it finishes", http.StatusConflict)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), volumeBackupLimit)
	defer cancel()
	f, err := openVolumeBackup(ctx, b)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()
	if err := checkVolumeArchive(f); err != nil {
		http.Error(w, "the backup file is damaged, so nothing was changed: "+err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// The open file stays readable even if this backup prunes it.
	if _, err := p.runVolumeBackup(src, b.Volume, "pre-restore"); err != nil {
		http.Error(w, "couldn't back up the current files first, so nothing was changed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := p.restoreVolume(ctx, b, f); err != nil {
		http.Error(w, "restore failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	p.audit(r, "volume.restore", b.App, b.Volume)
	w.WriteHeader(http.StatusNoContent)
}

func (p *Panel) handleVolumeBackupDownload(w http.ResponseWriter, r *http.Request) {
	b, ok := getVolumeBackup(sanitizeID(r.URL.Query().Get("id")))
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if !p.canAccess(r, b.App) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	f, err := os.Open(volumeBackupPath(b))
	if err != nil {
		http.Error(w, "backup file is missing", http.StatusNotFound)
		return
	}
	defer f.Close()
	p.audit(r, "volume.download", b.App, b.Volume)
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%s-%d.tar.gz"`, b.App, b.Volume, b.Created))
	if b.Size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(b.Size, 10))
	}
	_, _ = io.Copy(w, f)
}

func lastVolumeBackupAt(app, volume string) int64 {
	var t int64
	_ = sqlDB.QueryRow(`SELECT COALESCE(MAX(created), 0) FROM volume_backups WHERE app=? AND volume=?`, app, volume).Scan(&t)
	return t
}

func (p *Panel) volumeBackupLoop() {
	time.Sleep(3 * time.Minute)
	for {
		guard("volumeBackupLoop", func() {
			now := time.Now().Unix()
			for _, src := range allSources() {
				if src.Parent != "" || isPreviewStack(src.Stack) {
					continue
				}
				for _, v := range p.volumeDefs(src) {
					if now-lastVolumeBackupAt(src.App, v.Name) < 24*3600 {
						continue
					}
					if p.volumeSizes()[config.VolumeName(src.App, v.Name)] == 0 {
						continue
					}
					if _, err := p.runVolumeBackup(src, v.Name, "scheduled"); err != nil {
						log.Printf("skiff: scheduled backup of %s/%s failed: %v", src.App, v.Name, err)
					}
				}
			}
		})
		time.Sleep(time.Hour)
	}
}
