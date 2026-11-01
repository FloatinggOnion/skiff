package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nuelScript/skiff/internal/config"
	"github.com/nuelScript/skiff/internal/docker"
	"github.com/nuelScript/skiff/internal/ui"
	"github.com/spf13/cobra"
)

func newRollbackCmd() *cobra.Command {
	var configPath string
	var image string
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:    "rollback",
		Short:  "Instantly re-run a retained build, with no rebuild",
		Hidden: true, // internal: the panel drives this from deploy history
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRollback(configPath, image, timeout)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", config.DefaultFile, "path to skiff.toml")
	cmd.Flags().StringVar(&image, "image", "", "retained image tag to run (skiff-<app>:<deployid>)")
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "how long to wait for the version to become healthy (default: health_timeout in skiff.toml, else 30s)")
	return cmd
}

func runRollback(configPath, image string, timeout time.Duration) error {
	if image == "" {
		return fmt.Errorf("--image is required")
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		ui.Fail("Couldn't load config")
		return err
	}

	eng := docker.For(cfg.RemoteHost())
	if err := eng.Available(); err != nil {
		ui.Fail(err.Error())
		return err
	}
	if !eng.ImageExists(image) {
		ui.Fail("That build is no longer available to roll back to")
		return fmt.Errorf("image %s not found", image)
	}

	ui.Banner(version)
	fmt.Println("  " + ui.Accent("Rolling back "+cfg.Name))
	fmt.Println()

	return releaseImage(eng, cfg, releaseSpec{
		image: image, contextDir: filepath.Dir(configPath), timeout: timeout, start: time.Now(),
		labels: map[string]string{"skiff.deploy": os.Getenv("SKIFF_DEPLOY_ID")},
	})
}
