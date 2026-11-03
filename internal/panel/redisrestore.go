package panel

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nuelScript/skiff/internal/docker"
)

func (e dbEngine) canRestore() bool { return e.restoreCmd != nil || e.offlineRestore }

// redisAuth passes the password in the environment, where a process listing can't see it.
func redisAuth(pass string) string {
	if pass == "" {
		return ""
	}
	return "export REDISCLI_AUTH='" + strings.ReplaceAll(pass, "'", `'\''`) + "'; "
}

const redisCheckScript = `set -e; cat > /tmp/in.rdb; redis-check-rdb /tmp/in.rdb >/dev/null || { echo "the backup file is damaged" >&2; exit 1; }`

// Redis ignores dump.rdb when its append-only log is on and starts empty, so the dump is loaded with the log off and the log rebuilt from it. If anything fails, the old files go back.
const redisLoadScript = `cat > /tmp/in.rdb
fail() { echo "$1" >&2; redis-cli -p 6399 shutdown nosave >/dev/null 2>&1; rm -rf /data/appendonlydir /data/dump.rdb; mv /data/.restore-old/* /data/ 2>/dev/null; rmdir /data/.restore-old; exit 1; }
rm -rf /data/.restore-old; mkdir /data/.restore-old
for f in appendonlydir dump.rdb; do [ ! -e "/data/$f" ] || mv "/data/$f" /data/.restore-old/; done
cp /tmp/in.rdb /data/dump.rdb || fail "couldn't write the backup"
redis-server --dir /data --appendonly no --port 6399 --logfile /tmp/redis.log >/dev/null &
pid=$!
until redis-cli -p 6399 ping 2>/dev/null | grep -q PONG; do kill -0 $pid 2>/dev/null || fail "Redis couldn't load the backup"; sleep 0.2; done
redis-cli -p 6399 config set appendonly yes >/dev/null || fail "couldn't switch the log on"
info() { redis-cli -p 6399 info persistence; }
i=0; until s=$(info) && echo "$s" | grep -q "aof_enabled:1" && echo "$s" | grep -q "aof_rewrite_in_progress:0" && echo "$s" | grep -q "aof_rewrite_scheduled:0" && ! echo "$s" | grep -q "aof_rewrites:0"; do i=$((i+1)); [ $i -lt 3000 ] || fail "couldn't rebuild the log"; sleep 0.2; done
redis-cli -p 6399 shutdown nosave >/dev/null 2>&1
rm -rf /data/.restore-old`

// restoreRedis swaps the data files while Redis is stopped. The dump is checked first, so a damaged file costs no downtime.
func (p *Panel) restoreRedis(ctx context.Context, d dbRow, dump io.Reader) error {
	dir := filepath.Join(backupsDir(), d.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "restore-*.rdb")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if _, err := io.Copy(tmp, dump); err != nil {
		return err
	}
	rewind := func() (io.Reader, error) {
		_, err := tmp.Seek(0, io.SeekStart)
		return tmp, err
	}
	image := d.image()
	in, err := rewind()
	if err != nil {
		return err
	}
	if err := p.eng.RunInput(ctx, image, nil, redisCheckScript, in); err != nil {
		return err
	}
	if err := p.eng.Stop(d.Container); err != nil {
		return fmt.Errorf("stopping Redis: %w", err)
	}
	in, err = rewind()
	if err != nil {
		return err
	}
	loadErr := p.eng.RunInput(ctx, image, []docker.Mount{{Name: d.volume(), Destination: "/data"}}, redisLoadScript, in)
	if err := p.eng.Start(d.Container); err != nil {
		return fmt.Errorf("starting Redis again: %w", err)
	}
	if err := p.waitRedis(ctx, d); err != nil {
		return err
	}
	return loadErr
}

// waitRedis returns once Redis answers, so callers don't report success while it's still loading.
func (p *Panel) waitRedis(ctx context.Context, d dbRow) error {
	cmd := []string{"sh", "-c", redisAuth(d.Password) + "redis-cli --no-auth-warning ping"}
	var last error
	for range 300 {
		var out strings.Builder
		last = p.eng.Exec(ctx, d.Container, cmd, nil, &out)
		if last == nil && strings.Contains(out.String(), "PONG") {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("Redis didn't come back after the restore: %v", last)
}
