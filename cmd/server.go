package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/nuelScript/skiff/internal/ui"
	"github.com/spf13/cobra"
)

func newServerCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "server",
		Short: "Manage deploy servers",
	}
	c.AddCommand(newServerSetupCmd())
	c.AddCommand(newServerRestoreCmd())
	c.AddCommand(newServerReset2FACmd())
	return c
}

func newServerSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup <user@host>",
		Short: "Install Docker on a fresh server over SSH",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			host := args[0]

			ui.Banner(version)
			ui.Field("server", host)
			fmt.Println()

			ui.Step("Checking SSH access")
			if err := exec.Command("ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=15", host, "true").Run(); err != nil {
				ui.Fail("Can't SSH to " + host + " — set up key-based SSH first")
				return err
			}
			ui.Done("SSH works")

			if exec.Command("ssh", host, "docker version").Run() == nil {
				ui.Done("Docker already installed")
			} else {
				ui.Step("Installing Docker")
				install := exec.Command("ssh", host, "curl -fsSL https://get.docker.com | sh")
				install.Stdout = os.Stdout
				install.Stderr = os.Stderr
				if err := install.Run(); err != nil {
					ui.Fail("Docker install failed")
					return err
				}
				ui.Done("Docker installed")
			}

			fmt.Println()
			ui.Note("ready — set [server] host = \"" + host + "\" in skiff.toml, then `skiff deploy`")
			fmt.Println()
			return nil
		},
	}
}
