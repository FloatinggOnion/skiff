package cmd

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/nuelScript/skiff/internal/builder"
	"github.com/nuelScript/skiff/internal/config"
	"github.com/nuelScript/skiff/internal/docker"
	"github.com/nuelScript/skiff/internal/drain"
	"github.com/nuelScript/skiff/internal/health"
	"github.com/nuelScript/skiff/internal/proxy"
	"github.com/nuelScript/skiff/internal/registry"
	"github.com/nuelScript/skiff/internal/ui"
	"github.com/spf13/cobra"
)

const defaultBuildTimeout = 15 * time.Minute

func newDeployCmd() *cobra.Command {
	var configPath string
	var timeout time.Duration
	var buildTimeout time.Duration
	var name, buildContext string
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Build and deploy the current app to your server",
		Long: `Deploy builds your app, runs it, and serves it at a URL.
Reads config from skiff.toml.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeploy(configPath, buildContext, timeout, buildTimeout, name)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", config.DefaultFile, "path to skiff.toml")
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "how long to wait for the new version to become healthy (default: health_timeout in skiff.toml, else 30s)")
	cmd.Flags().DurationVar(&buildTimeout, "build-timeout", defaultBuildTimeout, "cancel the build if it runs longer than this")
	cmd.Flags().StringVar(&name, "name", "", "override the app name (e.g. for preview environments)")
	cmd.Flags().StringVar(&buildContext, "context", "", "directory to build from (default: the directory holding skiff.toml)")
	return cmd
}

func runDeploy(configPath, buildContext string, timeout, buildTimeout time.Duration, nameOverride string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		ui.Fail("Couldn't load config")
		return err
	}
	if nameOverride != "" {
		cfg.Name = nameOverride
	}

	eng := docker.For(cfg.RemoteHost())

	ui.Banner(version)
	fmt.Println("  " + ui.Accent("Deploying "+cfg.Name))
	fmt.Println()

	contextDir := buildContext
	if contextDir == "" {
		contextDir = filepath.Dir(configPath)
	}
	prebuilt := cfg.Build.Image
	var b builder.Builder
	switch bc := cfg.Build; {
	case prebuilt != "":
	case bc.Start != "" || bc.Static != "":
		b = builder.Custom(bc.Base, bc.Install, bc.Build, bc.Start, bc.Static)
	default:
		if os.Getenv("SKIFF_DEPLOY_ID") != "" && !dockerfileInside(cfg.Build.Dockerfile) {
			err = fmt.Errorf("the dockerfile must be a path inside the build folder")
			ui.Fail(err.Error())
			return err
		}
		b, err = builder.Select(contextDir, cfg.Build.Dockerfile)
		if err != nil {
			ui.Fail(err.Error())
			return err
		}
	}

	ui.Field("target", cfg.TargetLabel())
	if prebuilt != "" {
		ui.Field("image", prebuilt)
	} else {
		ui.Field("build", b.Name())
	}
	panel, signedIn := panelFor(cfg)
	if signedIn {
		if n := pullDashboardEnv(panel, cfg, contextDir); n > 0 {
			ui.Field("env", fmt.Sprintf("%d from the dashboard", n))
		}
	}
	fmt.Println()

	if err := eng.Available(); err != nil {
		ui.Fail(err.Error())
		return err
	}

	start := time.Now()
	image := fmt.Sprintf("skiff-%s:latest", cfg.Name)

	// A panel that dies takes our output with it; the deploy should still finish rather than die partway through a release.
	if os.Getenv("SKIFF_DEPLOY_ID") != "" {
		signal.Ignore(syscall.SIGPIPE)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if buildTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, buildTimeout)
		defer cancel()
	}

	if prebuilt != "" {
		if err := pullPrebuilt(ctx, eng, prebuilt, image, buildTimeout); err != nil {
			return err
		}
	} else {
		dockerfile, err := b.Dockerfile(cfg.Build.Port, cfg.Environment(contextDir))
		if err != nil {
			ui.Fail("Couldn't prepare the build")
			return err
		}
		ui.Step("Building " + image)
		// Builds the panel runs for a team pull their base images, so they can't use another team's local ones.
		onPanel := os.Getenv("SKIFF_DEPLOY_ID") != ""
		out := &pullSniffer{w: termSafe(os.Stdout)}
		if err := eng.BuildFromDockerfile(ctx, image, dockerfile, contextDir, onPanel, out); err != nil {
			switch ctx.Err() {
			case context.DeadlineExceeded:
				ui.Fail(fmt.Sprintf("Build exceeded %s — canceled", buildTimeout))
			case context.Canceled:
				ui.Fail("Build canceled")
			default:
				ui.Fail("Build failed")
				if onPanel && out.denied {
					ui.Note("A base image couldn't be pulled. Builds here use images from a registry, not images stored on this server: check the name in your Dockerfile, and sign in if it's private.")
				}
			}
			return err
		}
		ui.Done("Built " + image)
	}

	deployID := os.Getenv("SKIFF_DEPLOY_ID")
	labels := map[string]string{}
	if deployID == "" {
		deployID = fmt.Sprintf("%d-%s", time.Now().Unix(), shortID())
		labels = cliDeployLabels(contextDir)
	}
	labels["skiff.deploy"] = deployID

	if err := releaseImage(eng, cfg, releaseSpec{image: image, contextDir: contextDir, timeout: timeout, start: start, labels: labels}); err != nil {
		return err
	}

	retainRollbackImage(eng, cfg.Name, image, deployID)
	if signedIn {
		reportDeploy(panel, cfg, deployID, labels)
	}
	return nil
}

// dockerfileInside reports whether a dockerfile path stays within the build folder.
func dockerfileInside(p string) bool {
	if filepath.IsAbs(p) {
		return false
	}
	return !slices.Contains(strings.Split(filepath.ToSlash(p), "/"), "..")
}

// pullSniffer passes build output through and notes when an image couldn't be pulled.
type pullSniffer struct {
	w      io.Writer
	denied bool
}

func (s *pullSniffer) Write(b []byte) (int, error) {
	if bytes.Contains(b, []byte("pull access denied")) {
		s.denied = true
	}
	return s.w.Write(b)
}

// pullPrebuilt fetches a prebuilt image and tags it as the app's build, so the release and rollback paths treat it like one Skiff built.
func pullPrebuilt(ctx context.Context, eng *docker.Engine, ref, image string, timeout time.Duration) error {
	ui.Step("Pulling " + ref)
	var auth *docker.RegistryAuth
	if user := os.Getenv("SKIFF_REGISTRY_USER"); user != "" {
		auth = &docker.RegistryAuth{Host: config.RegistryHost(ref), Username: user, Password: os.Getenv("SKIFF_REGISTRY_PASSWORD")}
	}
	var out strings.Builder
	if err := eng.Pull(ctx, ref, auth, &out); err != nil {
		switch ctx.Err() {
		case context.DeadlineExceeded:
			ui.Fail(fmt.Sprintf("Pull exceeded %s — canceled", timeout))
		case context.Canceled:
			ui.Fail("Pull canceled")
		default:
			ui.Fail("Couldn't pull " + ref)
			if s := strings.TrimSpace(out.String()); s != "" {
				fmt.Println(s)
			}
			if auth == nil {
				ui.Note("for a private image, sign in to its registry first (docker login), or set SKIFF_REGISTRY_USER and SKIFF_REGISTRY_PASSWORD")
			}
		}
		return err
	}
	if err := eng.Tag(ref, image); err != nil {
		ui.Fail("Couldn't tag " + ref)
		return err
	}
	if d := eng.ImageDigest(ref); d != "" {
		ui.Field("digest", d)
	}
	ui.Done("Pulled " + ref)
	return nil
}

// cliDeployLabels marks a deploy run by hand so the server's panel can list it, with the commit it came from when there is one.
func cliDeployLabels(dir string) map[string]string {
	labels := map[string]string{"skiff.via": "cli"}
	git := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	if sha := git("rev-parse", "HEAD"); sha != "" {
		labels["skiff.commit"] = sha
		if msg := git("log", "-1", "--format=%s"); msg != "" {
			if len(msg) > 200 {
				msg = msg[:200]
			}
			labels["skiff.message"] = msg
		}
	}
	return labels
}

type releaseSpec struct {
	image      string
	contextDir string
	timeout    time.Duration
	start      time.Time
	labels     map[string]string
}

// releaseImage brings a built image up healthy, atomically swaps the router to it, then retires the old — the blue-green path shared by deploy and rollback.
func releaseImage(eng *docker.Engine, cfg *config.Config, spec releaseSpec) error {
	image, contextDir, timeout, start := spec.image, spec.contextDir, spec.timeout, spec.start
	if timeout <= 0 {
		timeout = health.DefaultTimeout
		if cfg.Deploy.HealthTimeout > 0 {
			timeout = time.Duration(cfg.Deploy.HealthTimeout) * time.Second
		}
	}
	if cfg.Build.Port == 0 {
		cfg.Build.Port = eng.ImagePort(image)
		if cfg.Build.Port == 0 {
			cfg.Build.Port = config.DefaultPort
		}
		ui.Field("port", fmt.Sprintf("%d", cfg.Build.Port))
	}
	labels := map[string]string{}
	for k, v := range spec.labels {
		labels[k] = v
	}
	// Recorded on the container so the server's panel checks scale-ups of a CLI app the same way.
	labels["skiff.health"] = cfg.Deploy.HealthPath
	labels["skiff.memory"] = cfg.Resources.Memory
	labels["skiff.cpu"] = cfg.Resources.CPU
	// Runtime env = build env + secrets; secrets never bake into the image, so a rollback runs current secrets against the old code.
	buildEnv := cfg.Environment(contextDir)
	runtimeEnv := make(map[string]string, len(buildEnv)+len(cfg.Secrets))
	for k, v := range buildEnv {
		runtimeEnv[k] = v
	}
	for k, v := range cfg.Secrets {
		runtimeEnv[k] = v
	}

	// Join the app's network to reach managed DBs by name (team-private, else shared "skiff"); fall back to no network rather than fail the deploy.
	netName := cfg.Deploy.Network
	if netName == "" {
		netName = "skiff"
	}
	network := ""
	if eng.EnsureNetwork(netName) == nil {
		network = netName
	}

	var mounts []docker.Mount
	for _, v := range cfg.Volumes {
		name := config.VolumeName(cfg.Name, v.Name)
		if err := eng.EnsureAppVolume(name, cfg.Name, v.Name); err != nil {
			ui.Fail("Couldn't create volume " + v.Name)
			return err
		}
		mounts = append(mounts, docker.Mount{Name: name, Destination: v.Path})
	}

	// Release command (e.g. migrations) runs once before the new version goes live; on failure abort — the old version keeps serving.
	if rel := strings.TrimSpace(cfg.Deploy.Release); rel != "" {
		ui.Step("Running release command")
		rctx, cancel := context.WithTimeout(context.Background(), timeout)
		out, err := eng.RunOnce(rctx, image, runtimeEnv, network, mounts, rel)
		cancel()
		if err != nil {
			ui.Fail("Release command failed — deploy aborted, old version still serving")
			if s := strings.TrimSpace(out); s != "" {
				fmt.Println(s)
			}
			return fmt.Errorf("release command failed: %w", err)
		}
		ui.Done("Release command ran")
	}

	replicas := cfg.Replicas
	if replicas < 1 {
		replicas = 1
	}
	if replicas > 1 {
		ui.Step(fmt.Sprintf("Starting new version · %d replicas", replicas))
	} else {
		ui.Step("Starting new version")
	}
	if cfg.Deploy.HealthPath != "" {
		ui.Field("health", fmt.Sprintf("GET %s · %s", cfg.Deploy.HealthPath, timeout))
	}

	// Bring the whole new set up healthy before touching the old; if any replica fails, roll back everything started — the old version keeps serving.
	var reps []registry.Replica
	var held []string
	release := func() {
		for _, c := range held {
			_ = drain.Remove(c)
		}
		held = nil
	}
	defer release()
	rollback := func() {
		for _, rp := range reps {
			_ = eng.Remove(rp.Container)
		}
	}
	// With no router on a remote box, the published port is the only way in.
	direct := false
	if eng.IsRemote() {
		running, known := docker.RouterOn(cfg.RemoteHost())
		direct = known && !running
	}
	for range replicas {
		container := fmt.Sprintf("%s-%s", cfg.Name, shortID())
		if !eng.IsRemote() {
			now := time.Now().Unix()
			if drain.Add(drain.Entry{App: cfg.Name, Container: container, Since: now, Until: now + int64(timeout/time.Second) + startingMargin, Starting: true}) == nil {
				held = append(held, container)
			}
		}
		hostPort, err := eng.Run(docker.RunSpec{
			Name:          container,
			App:           cfg.Name,
			Image:         image,
			ContainerPort: cfg.Build.Port,
			Public:        direct,
			Memory:        cfg.Resources.Memory,
			CPU:           cfg.Resources.CPU,
			Env:           runtimeEnv,
			Network:       network,
			Labels:        labels,
			Volumes:       mounts,
			Aliases:       cfg.Deploy.Aliases,
			Command:       cfg.Deploy.Command,
			Options:       config.DecodeRunOptions(cfg.Deploy.RunOptions),
		})
		if err != nil {
			crashed := keptExiting(eng, container)
			output := lastOutput(eng, container)
			_ = eng.Remove(container)
			rollback()
			ui.Fail("Couldn't start container")
			showOutput(output)
			if crashed {
				ui.Note("The app kept exiting, so it never answered.")
			}
			return err
		}
		healthHost, healthPort, stopReach, err := reach(eng, cfg.RemoteHost(), hostPort)
		if err == nil {
			err = health.WaitMode(cfg.Deploy.HealthMode, healthHost, healthPort, cfg.Deploy.HealthPath, timeout)
			stopReach()
		}
		if err != nil {
			crashed := keptExiting(eng, container)
			output := lastOutput(eng, container)
			_ = eng.Remove(container)
			rollback()
			ui.Fail("New version never became healthy — rolled back")
			ui.Note(err.Error())
			showOutput(output)
			if crashed {
				ui.Note("The app kept exiting, so it never answered.")
			} else if cfg.Deploy.HealthPath != "" {
				ui.Note(fmt.Sprintf("%s must answer 2xx or 3xx on 0.0.0.0:%d (the `port` in skiff.toml)", cfg.Deploy.HealthPath, cfg.Build.Port))
			} else {
				ui.Note(fmt.Sprintf("the app must listen on 0.0.0.0:%d (the `port` in skiff.toml) and answer HTTP", cfg.Build.Port))
			}
			return err
		}
		reps = append(reps, registry.Replica{Container: container, HostPort: hostPort})
	}
	ui.Done("Healthy")

	entry := registry.App{
		Name:      cfg.Name,
		Container: reps[0].Container,
		Port:      cfg.Build.Port,
		HostPort:  reps[0].HostPort,
		Replicas:  reps,
	}
	if eng.IsRemote() {
		// The server reconciles its containers against its own registry, so an
		// app recorded only here would be treated as an orphan there.
		if err := registry.PutRemote(cfg.RemoteHost(), entry); err != nil {
			rollback()
			ui.Fail("Couldn't record the app on the server — rolled back, old version still serving")
			return err
		}
		entry.Host = cfg.RemoteHost()
	}
	if err := registry.Put(entry); err != nil {
		ui.Fail("Couldn't update the registry")
		return err
	}

	// Retire every container for this app not in the new set — the previous version plus leftovers from a failed swap or canceled build.
	newSet := map[string]bool{}
	for _, rp := range reps {
		newSet[rp.Container] = true
	}
	release()
	retired, draining := retireOld(eng, cfg, newSet)
	if draining > 0 {
		ui.Done(fmt.Sprintf("Previous version is finishing open requests, for up to %s", time.Duration(cfg.Deploy.DrainSeconds())*time.Second))
	} else if retired > 0 {
		ui.Done("Retired the previous version")
	}

	fmt.Println()
	if eng.IsRemote() && direct {
		ui.Live(fmt.Sprintf("http://%s:%d", docker.SSHHostname(cfg.RemoteHost()), reps[0].HostPort), time.Since(start))
	} else if eng.IsRemote() {
		ui.Done(fmt.Sprintf("Running on %s, served by its router (%.1fs)", cfg.RemoteHost(), time.Since(start).Seconds()))
	} else {
		ui.Live(proxy.URL(cfg.Name), time.Since(start))
		ui.Field("direct", fmt.Sprintf("http://localhost:%d", reps[0].HostPort))
	}
	return nil
}

// startingMargin is added to the health timeout for pulling and starting the container.
const startingMargin = 120

// retireOld removes the app's other containers, or leaves the running ones to the router so open requests can finish. That needs a router here that says it's up, because nothing else would stop them or keep new traffic away.
func retireOld(eng *docker.Engine, cfg *config.Config, keep map[string]bool) (retired, draining int) {
	seconds := cfg.Deploy.DrainSeconds()
	ports := map[string]int{}
	if seconds > 0 && !eng.IsRemote() && drain.RouterAlive() {
		if routes, err := eng.Routes(); err == nil {
			for _, r := range routes {
				if r.App == cfg.Name {
					ports[r.Container] = r.HostPort
				}
			}
		}
	}
	now := time.Now()
	finishing := drain.Active(now)
	for _, name := range eng.AppContainers(cfg.Name) {
		if keep[name] || finishing[name] {
			continue
		}
		if port := ports[name]; port > 0 {
			err := drain.Add(drain.Entry{App: cfg.Name, Container: name, HostPort: port, Since: now.Unix(), Until: now.Unix() + int64(seconds)})
			if err == nil {
				draining++
				continue
			}
		}
		_ = eng.Stop(name)
		_ = eng.Remove(name)
		retired++
	}
	return retired, draining
}

// retainImages is how many past builds to keep as instant-rollback points, per app.
const retainImages = 5

// retainRollbackImage tags the build by deploy id for no-rebuild rollback and prunes old ones.
func retainRollbackImage(eng *docker.Engine, app, image, did string) {
	if err := eng.Tag(image, fmt.Sprintf("skiff-%s:%s", app, did)); err != nil {
		return
	}
	tags := eng.AppImageTags(app) // newest first
	for i := retainImages; i < len(tags); i++ {
		_ = eng.RemoveImage(fmt.Sprintf("skiff-%s:%s", app, tags[i]))
	}
}

func shortID() string {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000)
	}
	return hex.EncodeToString(b)
}

// lastOutput reads a failed container's output before it's removed, because this is the only chance to.
func lastOutput(eng *docker.Engine, container string) string {
	if eng.State(container) == "missing" {
		return ""
	}
	var buf bytes.Buffer
	_ = eng.Logs(container, false, "30", &buf)
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	for i, line := range lines {
		if len(line) > 500 {
			lines[i] = line[:500] + "…"
		}
	}
	return strings.Join(lines, "\n")
}

func showOutput(output string) {
	if output == "" {
		ui.Note("The app printed nothing.")
		return
	}
	ui.Note("Last output from the app:")
	for _, line := range strings.Split(output, "\n") {
		ui.Note("  " + stripControl(line))
	}
}

// keptExiting checks restarts as well as state, because a crashing app is briefly running between attempts.
func keptExiting(eng *docker.Engine, container string) bool {
	state := eng.State(container)
	return state != "missing" && (state != "running" || eng.Restarts(container) > 1)
}
