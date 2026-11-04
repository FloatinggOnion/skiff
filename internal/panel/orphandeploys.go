package panel

import (
	"fmt"
	"log"
	"os"
	"syscall"
	"time"

	"github.com/nuelScript/skiff/internal/drain"
	"github.com/nuelScript/skiff/internal/registry"
)

const orphanDeployEvery = 15 * time.Second

// setDeployOwner marks a deploy as run by this panel. A self-deploy isn't: it outlives the panel that started it on purpose.
func setDeployOwner(id string) {
	_, _ = sqlDB.Exec(`UPDATE deploys SET panel_pid=? WHERE id=?`, os.Getpid(), id)
}

func setDeployChild(id string, pid int) {
	_, _ = sqlDB.Exec(`UPDATE deploys SET child_pid=? WHERE id=?`, pid, id)
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

type orphanDeploy struct {
	id, app            string
	started            int64
	panelPID, childPID int
}

func buildingDeploys() []orphanDeploy {
	rows, err := sqlDB.Query(`SELECT id,app,started,panel_pid,child_pid FROM deploys WHERE status='building' AND panel_pid<>0`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []orphanDeploy
	for rows.Next() {
		var d orphanDeploy
		if rows.Scan(&d.id, &d.app, &d.started, &d.panelPID, &d.childPID) == nil {
			out = append(out, d)
		}
	}
	return out
}

// stopOrphanDeploys cancels the build a dead panel left running for this app, so it can't race the deploy that's starting.
func stopOrphanDeploys(app, id string) {
	for _, d := range buildingDeploys() {
		if d.app == app && d.id != id && !processAlive(d.panelPID) && processAlive(d.childPID) {
			_ = syscall.Kill(d.childPID, syscall.SIGINT)
		}
	}
}

// settleOrphanDeploys gives a deploy whose panel died its real outcome once its own process has ended too. A panel that restarts while it's still going, or during a blue-green swap, leaves it alone until then.
func (p *Panel) settleOrphanDeploys() {
	for _, d := range buildingDeploys() {
		if d.panelPID == os.Getpid() || processAlive(d.panelPID) || processAlive(d.childPID) {
			continue
		}
		p.settleOrphanDeploy(d)
	}
}

func (p *Panel) settleOrphanDeploy(d orphanDeploy) {
	current := map[string]bool{}
	if apps, err := registry.Load(); err == nil {
		for _, r := range apps[d.app].Replicas {
			current[r.Container] = true
		}
		current[apps[d.app].Container] = true
	}
	status := "failed"
	for _, c := range p.eng.ContainersLabelled("skiff.deploy=" + d.id) {
		if current[c] {
			status = "live"
			continue
		}
		_ = drain.Remove(c)
		_ = p.eng.Remove(c)
	}
	note := "✗ the panel restarted during this deploy, and it didn't finish. The previous version is still running."
	if status == "live" {
		note = "✓ live. The panel restarted during this deploy, and it finished anyway."
	}
	if f, err := os.OpenFile(logPath(d.app, d.id), os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		fmt.Fprintln(f, note)
		f.Close()
	}
	setDeployStatus(d.app, d.id, status)
	log.Printf("skiff: deploy %s of %s, cut short by a panel restart: %s", d.id, d.app, status)
	// The panel that was reporting to GitHub is gone, so the commit would stay pending.
	var sha string
	_ = sqlDB.QueryRow(`SELECT full_sha FROM deploys WHERE id=? AND app=?`, d.id, d.app).Scan(&sha)
	if src, ok := getSource(d.app); ok && sha != "" {
		go p.reportStatus(src, sha, status)
	}
	if status == "failed" {
		if src, ok := getSource(d.app); ok {
			go dispatchAlert(alertEvent{Team: src.Team, Kind: "deploy.failed", App: d.app,
				Title: "Deploy failed: " + d.app, Detail: "The panel restarted during this deploy, and it didn't finish."})
		}
	}
}

func (p *Panel) orphanDeployLoop() {
	for {
		guard("orphanDeploys", p.settleOrphanDeploys)
		time.Sleep(orphanDeployEvery)
	}
}
