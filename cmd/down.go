package cmd

import (
	"fmt"

	"github.com/nuelScript/skiff/internal/docker"
	"github.com/nuelScript/skiff/internal/registry"
	"github.com/nuelScript/skiff/internal/ui"
	"github.com/spf13/cobra"
)

func newDownCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down <app>",
		Short: "Stop and remove a deployed app",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			ui.Banner(version)

			apps, err := registry.Load()
			if err != nil {
				return err
			}
			container := name
			host := ""
			if app, ok := apps[name]; ok && app.Container != "" {
				container = app.Container
				host = app.Host
			}

			if err := docker.For(host).Remove(container); err != nil {
				ui.Note("container not removed: " + err.Error())
			} else {
				ui.Done("Stopped " + name)
			}
			if host != "" {
				if err := registry.DeleteRemote(host, name); err != nil {
					ui.Note("server registry not updated: " + err.Error())
				}
			}

			existed, err := registry.Delete(name)
			if err != nil {
				return err
			}
			if existed {
				ui.Done("Removed " + name + " from the registry")
			} else {
				ui.Note("no app named " + name + " in the registry")
			}
			fmt.Println()
			return nil
		},
	}
}
