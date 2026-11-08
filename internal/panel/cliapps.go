package panel

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/nuelScript/skiff/internal/config"
	"github.com/nuelScript/skiff/internal/docker"
	"github.com/nuelScript/skiff/internal/registry"
)

const originCLI = "cli"

const cliOnlyMsg = "this app is deployed from the CLI — ship a new version with `skiff deploy`, or connect a repository to deploy it from here"

// adoptCLIApps gives every app `skiff deploy` put on this server a project in the founding team, so nothing runs here that the dashboard can't see.
func adoptCLIApps(apps map[string]registry.App, team string) []string {
	if team == "" {
		return nil
	}
	var adopted []string
	for name, a := range apps {
		if a.Host != "" || name == "" {
			continue
		}
		if _, ok := getSource(name); ok {
			continue
		}
		replicas := len(a.Replicas)
		if replicas < 1 {
			replicas = 1
		}
		src := Source{
			App: name, Team: team, Port: strconv.Itoa(a.Port), Origin: originCLI,
			Replicas: replicas, ScaleMin: 1, ScaleMax: 1,
		}
		if envStage.heldByOther(name, team, time.Now()) {
			_ = setEnv(name, nil)
		}
		if putSource(src) == nil {
			envStage.release(name)
			adopted = append(adopted, name)
		}
	}
	return adopted
}

// recordCLIDeploy adds the running version of a CLI app to its deploy history the first time the panel sees it.
func (p *Panel) recordCLIDeploy(a registry.App, meta docker.ContainerMeta) {
	id := sanitizeID(meta.Labels["skiff.deploy"])
	if id == "" {
		id = "cli-" + sanitizeID(a.Container)
	}
	if deployStatus(a.Name, id) != "" {
		return
	}
	started := meta.Created.Unix()
	if meta.Created.IsZero() {
		started = time.Now().Unix()
	}
	addDeploy(Deploy{
		ID: id, App: a.Name, Commit: shortCommit(meta.Labels["skiff.commit"]), Message: meta.Labels["skiff.message"],
		Trigger: originCLI, Status: "live", Started: started,
	})
	writeCLIDeployLog(a.Name, id, meta.Created)
}

func writeCLIDeployLog(app, id string, at time.Time) {
	path := logPath(app, id)
	if os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	msg := fmt.Sprintf("Deployed with `skiff deploy` at %s.\nThe build ran on the machine that deployed it, so its output isn't stored here.\n",
		at.UTC().Format("2006-01-02 15:04 UTC"))
	_ = os.WriteFile(path, []byte(msg), 0o644)
}

func (p *Panel) syncCLIApps() {
	apps, err := registry.Load()
	if err != nil {
		return
	}
	team := foundingTeam()
	for _, name := range adoptCLIApps(apps, team) {
		log.Printf("skiff: added CLI-deployed app %q to the dashboard", name)
		recordAudit(team, AuditEntry{Actor: "cli", Action: "project.adopt", Target: name, Detail: "deployed from the CLI"})
	}
	for name, a := range apps {
		src, ok := getSource(name)
		if !ok || src.Origin != originCLI || a.Container == "" {
			continue
		}
		meta, err := p.eng.ContainerMeta(a.Container)
		if err != nil {
			continue
		}
		p.recordCLIDeploy(a, meta)
		syncTomlWork(src, meta.Labels)
	}
}

func (p *Panel) cliAppLoop() {
	for {
		guard("syncCLIApps", p.syncCLIApps)
		time.Sleep(30 * time.Second)
	}
}

// appRuntime is the env and network to start more of an app's containers with. A CLI app's config lives in the skiff.toml on the machine that deployed it, so its running container is the only copy the server has.
// appSettings is what the app's workers and jobs run with: its environment, network, volumes, limits and start settings. A CLI app's limits come from its skiff.toml, recorded on its running container. The network is created if it's missing.
func (p *Panel) appSettings(src Source) docker.Settings {
	env, net := p.appRuntime(src)
	_ = p.eng.EnsureNetwork(net)
	var container string
	if apps, err := registry.Load(); err == nil {
		container = apps[src.App].Container
	}
	memory, cpu := p.replicaLimits(src, container)
	return docker.Settings{
		Env: env, Network: net, Volumes: p.appMounts(src),
		Memory: memory, CPU: cpu, Options: config.DecodeRunOptions(src.RunOptions),
	}
}

func (p *Panel) appRuntime(src Source) (map[string]string, string) {
	if src.Origin == originCLI {
		if apps, err := registry.Load(); err == nil {
			if a, ok := apps[src.App]; ok && a.Container != "" {
				if rt, err := p.eng.Runtime(a.Container); err == nil {
					delete(rt.Env, "PORT")
					net := dbNetwork
					if len(rt.Networks) > 0 {
						net = rt.Networks[0]
					}
					return rt.Env, net
				}
			}
		}
	}
	env := map[string]string{}
	for _, e := range deployEnv(src) {
		env[e.Key] = e.Value
	}
	return env, appNetwork(src)
}
