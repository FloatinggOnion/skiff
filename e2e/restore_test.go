//go:build e2e

package e2e

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRestoreReturnsToBackup changes real databases after a backup, restores, and reads the data back.
func TestRestoreReturnsToBackup(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	bin := filepath.Join(root, "skiff")
	run(t, "", nil, "go", "build", "-o", bin, "..")
	protectExisting(t, home)
	panel := freeAddr(t)
	start(t, []string{"HOME=" + home, "SKIFF_PANEL_PASSWORD=e2e-setup"}, bin, "panel", "--addr", panel, "--domain", "localhost")
	waitHTTP(t, "http://"+panel+"/api/me")
	c := client(t)
	post(t, c, "http://"+panel+"/api/auth/setup", `{"secret":"e2e-setup","email":"e2e@example.com","name":"E2E","password":"e2e-password-1"}`)

	engines := []struct {
		engine, name string
		exec         func(ctr, stmt string) []string
		seed, change string
		state        string
		want         string
	}{
		{
			engine: "postgres", name: "pgrestore",
			exec: func(ctr, stmt string) []string {
				return []string{"exec", ctr, "psql", "-U", "skiff", "-d", "pgrestore", "-v", "ON_ERROR_STOP=1", "-qtAc", stmt}
			},
			seed:   "create table t(id int primary key, v text); insert into t values (1,'original'),(2,'original');",
			change: "update t set v='changed' where id=1; delete from t where id=2; insert into t values (3,'later'); create table later(id int); create schema extra;",
			state:  "select (select string_agg(id||v, ',' order by id) from t) || '|' || (select string_agg(tablename, ',' order by tablename) from pg_tables where schemaname not in ('pg_catalog','information_schema')) || '|' || (select count(*) from pg_namespace where nspname='extra')",
			want:   "1original,2original|t|0",
		},
		{
			engine: "mysql", name: "myrestore",
			exec: func(ctr, stmt string) []string {
				return []string{"exec", ctr, "sh", "-c", "MYSQL_PWD=$MYSQL_PASSWORD mysql -N -u skiff myrestore -e \"" + stmt + "\""}
			},
			seed:   "create table t(id int primary key, v text); insert into t values (1,'original'),(2,'original');",
			change: "update t set v='changed' where id=1; delete from t where id=2; insert into t values (3,'later'); create table later(id int); create view lv as select id from t;",
			state:  "select (select group_concat(concat(id,v) order by id) from t), (select group_concat(table_name order by table_name) from information_schema.tables where table_schema=database())",
			want:   "1original,2original\tt",
		},
		{
			engine: "mongodb", name: "morestore",
			exec: func(ctr, stmt string) []string {
				return []string{"exec", ctr, "sh", "-c", "mongosh --quiet -u skiff -p $MONGO_INITDB_ROOT_PASSWORD --authenticationDatabase admin morestore --eval '" + stmt + "'"}
			},
			seed:   `db.t.insertMany([{_id:1,v:"original"},{_id:2,v:"original"}])`,
			change: `db.t.updateOne({_id:1},{$set:{v:"changed"}});db.t.deleteOne({_id:2});db.t.insertOne({_id:3,v:"later"});db.later.insertOne({x:1})`,
			state:  `print(db.t.find().sort({_id:1}).toArray().map(d=>d._id+d.v).join(",")+"|"+db.getCollectionNames().sort().join(","))`,
			want:   "1original,2original|t",
		},
		{
			engine: "redis", name: "rdrestore",
			exec: func(ctr, stmt string) []string {
				pass, _ := exec.Command("docker", "inspect", "-f", "{{index .Config.Cmd 2}}", ctr).Output()
				return []string{"exec", ctr, "sh", "-c", "export REDISCLI_AUTH=" + strings.TrimSpace(string(pass)) + "; " + stmt}
			},
			seed:   "redis-cli SET a original | grep -q OK && redis-cli SET b original | grep -q OK",
			change: "redis-cli SET a changed && redis-cli DEL b && redis-cli SET later x",
			state:  `echo "$(redis-cli GET a)/$(redis-cli GET b)/$(redis-cli DBSIZE)"`,
			want:   "original/original/2",
		},
	}

	for _, e := range engines {
		t.Run(e.engine, func(t *testing.T) {
			var db struct{ ID string }
			postJSON(t, c, "http://"+panel+"/api/databases", fmt.Sprintf(`{"engine":%q,"name":%q}`, e.engine, e.name), &db)
			ctr := "skiff-db-" + db.ID
			t.Cleanup(func() {
				_ = exec.Command("docker", "rm", "-f", ctr).Run()
				_ = exec.Command("docker", "volume", "rm", ctr+"-data").Run()
			})
			sql := func(stmt string) (string, error) {
				out, err := exec.Command("docker", e.exec(ctr, stmt)...).CombinedOutput()
				return strings.TrimSpace(string(out)), err
			}
			// The official images restart once while initialising, so keep trying until the seed lands.
			eventually(t, 3*time.Minute, func() bool {
				_, err := sql(e.seed)
				return err == nil
			})
			if got, err := sql(e.state); err != nil || got != e.want {
				t.Fatalf("after seeding: %q %v", got, err)
			}

			var b struct{ ID string }
			postJSON(t, c, "http://"+panel+"/api/backups?db="+db.ID, "", &b)
			if out, err := sql(e.change); err != nil {
				t.Fatalf("changing data: %v: %s", err, out)
			}
			changed, _ := sql(e.state)
			if changed == e.want {
				t.Fatalf("the change did nothing: %q", changed)
			}

			post(t, c, "http://"+panel+"/api/backups/restore?id="+b.ID, "")
			if got, err := sql(e.state); err != nil || got != e.want {
				t.Fatalf("after restore: got %q, want %q (%v)", got, e.want, err)
			}

			var list []struct{ ID, Trigger string }
			getJSON(t, c, "http://"+panel+"/api/backups?db="+db.ID, &list)
			var undo string
			for _, x := range list {
				if x.Trigger == "pre-restore" {
					undo = x.ID
				}
			}
			if undo == "" {
				t.Fatalf("no backup taken before the restore: %+v", list)
			}
			post(t, c, "http://"+panel+"/api/backups/restore?id="+undo, "")
			if got, _ := sql(e.state); got != changed {
				t.Fatalf("undoing the restore: got %q, want %q", got, changed)
			}

			if e.engine == "redis" {
				// The restored data has to survive a restart: Redis reads its log, not the dump, on the way back up.
				post(t, c, "http://"+panel+"/api/backups/restore?id="+b.ID, "")
				run(t, "", nil, "docker", "restart", ctr)
				eventually(t, time.Minute, func() bool {
					got, _ := sql(e.state)
					return got == e.want
				})
				if out, err := sql(e.change); err != nil {
					t.Fatalf("changing data again: %v: %s", err, out)
				}
			}
			if e.engine != "postgres" && e.engine != "redis" {
				return
			}
			// A damaged dump must fail loudly and leave the data alone.
			files, _ := filepath.Glob(filepath.Join(home, ".skiff", "backups", db.ID, b.ID+"*"))
			if len(files) != 1 {
				t.Fatalf("backup file: %v", files)
			}
			dump, err := os.ReadFile(files[0])
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(files[0], append(dump[:len(dump)/2:len(dump)/2], []byte("\nTHIS IS NOT VALID;\n")...), 0o600); err != nil {
				t.Fatal(err)
			}
			resp, err := c.Post("http://"+panel+"/api/backups/restore?id="+b.ID, "application/json", http.NoBody)
			if err != nil {
				t.Fatal(err)
			}
			msg, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode < 400 {
				t.Fatalf("a damaged backup reported success: %d %s", resp.StatusCode, msg)
			}
			if got, _ := sql(e.state); got != changed {
				t.Fatalf("a failed restore changed the data: got %q, want %q", got, changed)
			}
		})
	}
}
