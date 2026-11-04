// Package cmd defines the skiff CLI: the root command and every subcommand.
package cmd

import (
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
)

// buildVersion is injected at release time via -ldflags "-X …/cmd.buildVersion=<tag>"; empty otherwise.
var buildVersion = ""

var version = resolveVersion()

// resolveVersion prefers the ldflags release tag, then a clean `go install` module version, else "dev".
func resolveVersion() string {
	if buildVersion != "" {
		return strings.TrimPrefix(buildVersion, "v")
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		// A clean `go install` gives "vX.Y.Z"; "(devel)" or a "+" pseudo-version means dev.
		if v := info.Main.Version; strings.HasPrefix(v, "v") && !strings.Contains(v, "+") {
			return strings.TrimPrefix(v, "v")
		}
	}
	return "dev"
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "skiff",
		Short: "Effortless deploys on your own server",
		Long: `Skiff deploys your apps to your own server with a single command:
build it, run it, and get an HTTPS URL. No cloud bill.`,
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPostRun: func(cmd *cobra.Command, _ []string) {
			switch cmd.Name() {
			case updateCheckCmd, "completion", "help":
				return
			}
			notifyUpdate(version)
		},
	}
	root.AddCommand(newUpdateCheckCmd())
	root.AddCommand(newInitCmd())
	root.AddCommand(newDeployCmd())
	root.AddCommand(newComposeCmd())
	root.AddCommand(newRollbackCmd())
	root.AddCommand(newGitSetupCmd())
	root.AddCommand(newServerCmd())
	root.AddCommand(newRouterCmd())
	root.AddCommand(newPanelCmd())
	root.AddCommand(newSelfUpdateCmd())
	root.AddCommand(newProxyCmd())
	root.AddCommand(newDashboardCmd())
	root.AddCommand(newLsCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newOpenCmd())
	root.AddCommand(newLogsCmd())
	root.AddCommand(newDBCmd())
	root.AddCommand(newDownCmd())
	root.AddCommand(newSyncCmd())
	root.AddCommand(newLoginCmd())
	root.AddCommand(newLogoutCmd())
	root.AddCommand(newWhoamiCmd())
	root.AddCommand(newMCPCmd())
	root.AddCommand(newVersionCmd())
	return root
}

func Execute() error {
	return newRootCmd().Execute()
}
