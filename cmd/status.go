package cmd

import (
	"fmt"
	"net/http"
	"time"

	"github.com/nuelScript/skiff/internal/docker"
	"github.com/nuelScript/skiff/internal/proxy"
	"github.com/nuelScript/skiff/internal/registry"
	"github.com/nuelScript/skiff/internal/ui"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [app]",
		Short: "Show the status of deployed apps",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			apps, err := registry.List()
			if err != nil {
				return err
			}

			ui.Banner(version)

			var only string
			if len(args) == 1 {
				only = args[0]
			}

			shown := 0
			for _, a := range apps {
				if only != "" && a.Name != only {
					continue
				}
				shown++

				state := docker.For(a.Host).State(a.Container)

				url := proxy.URL(a.Name)
				if a.Host != "" {
					url = "on " + a.Host
				}

				health := "—"
				if state == "running" {
					if probeApp(a) {
						health = "healthy"
					} else {
						health = "unreachable"
					}
				}
				ui.Field(a.Name, fmt.Sprintf("%-8s %-12s %s", state, health, url))
			}

			if shown == 0 {
				if only != "" {
					ui.Note("no app named " + only)
				} else {
					ui.Note("No apps yet — run `skiff deploy`.")
				}
			}
			fmt.Println()
			return nil
		},
	}
}

func probe(host string, hostPort int) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://%s:%d/", host, hostPort))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}
