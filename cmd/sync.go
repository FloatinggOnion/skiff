package cmd

import (
	"fmt"
	"time"

	"github.com/nuelScript/skiff/internal/docker"
	"github.com/nuelScript/skiff/internal/reconcile"
	"github.com/nuelScript/skiff/internal/registry"
	"github.com/nuelScript/skiff/internal/ui"
	"github.com/spf13/cobra"
)

func newSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Reconcile containers with the registry (prune orphans, adopt live apps, drop dead entries)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ui.Banner(version)

			res, err := reconcile.Reap(docker.Local(), time.Now())
			if err != nil {
				return err
			}
			changed := 0
			for _, c := range res.Removed {
				ui.Done("Pruned orphan container " + c)
				changed++
			}
			for _, app := range res.Adopted {
				ui.Done("Adopted running app " + app + " into the registry")
				changed++
			}

			apps, err := registry.List()
			if err != nil {
				return err
			}
			for _, a := range apps {
				if docker.For(a.Host).State(a.Container) == "missing" {
					if ok, _ := registry.Delete(a.Name); ok {
						ui.Done("Dropped dead app " + a.Name)
						changed++
					}
				}
			}

			if changed == 0 {
				ui.Note("Everything's in sync.")
			}
			fmt.Println()
			return nil
		},
	}
}
