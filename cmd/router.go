package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/nuelScript/skiff/internal/docker"
	"github.com/nuelScript/skiff/internal/edge"
	"github.com/nuelScript/skiff/internal/gate"
	"github.com/nuelScript/skiff/internal/router"
	"github.com/nuelScript/skiff/internal/sleep"
	"github.com/nuelScript/skiff/internal/ui"
	"github.com/spf13/cobra"
)

func newRouterCmd() *cobra.Command {
	var domain, httpAddr, panel, panelPointer, siteApp, domainsFile, metricsFile, requestsDir, mailFile, smtpAddr, certsDir string
	cmd := &cobra.Command{
		Use:   "router",
		Short: "Run the edge router (subdomain routing + auto HTTPS) — runs on the server",
		RunE: func(cmd *cobra.Command, args []string) error {
			if domain == "" {
				return fmt.Errorf("--domain is required (e.g. --domain useskiff.xyz)")
			}
			home, _ := os.UserHomeDir()
			if panelPointer == "" {
				panelPointer = filepath.Join(home, ".skiff", "panel.addr")
			}
			if domainsFile == "" {
				domainsFile = filepath.Join(home, ".skiff", "domains.json")
			}
			if metricsFile == "" {
				metricsFile = filepath.Join(home, ".skiff", "metrics.json")
			}
			if requestsDir == "" {
				requestsDir = filepath.Join(home, ".skiff", "requests")
			}
			if mailFile == "" {
				mailFile = filepath.Join(home, ".skiff", "mail.json")
			}
			if certsDir == "" {
				certsDir = defaultCertsDir(runtime.GOOS, home)
			}
			if httpAddr != "" && !cmd.Flags().Changed("smtp-addr") {
				smtpAddr = ""
			}
			rt := &router.Router{
				Domains:      strings.Split(domain, ","),
				Engine:       docker.Local(),
				Panel:        panel,
				PanelPointer: panelPointer,
				SiteApp:      siteApp,
				DomainsFile:  domainsFile,
				MetricsFile:  metricsFile,
				RequestsDir:  requestsDir,
				SleepFile:    sleep.DefaultFile(),
				MailFile:     mailFile,
				SMTPAddr:     smtpAddr,
				GateFile:     gate.DefaultFile(),
				EdgeFile:     edge.DefaultFile(),
			}

			ui.Banner(version)
			ui.Field("router", domain)
			if panel != "" {
				ui.Field("dashboard", "dash.* → "+panel)
			}
			if siteApp != "" {
				ui.Field("site", "apex + www → "+siteApp)
			}
			ui.Field("status", "status.* → live status page")
			if smtpAddr != "" {
				ui.Field("mail", "smtp "+smtpAddr+" as "+rt.MailHost()+" once forwarding is set up")
			}
			if httpAddr != "" {
				ui.Note("http-only test mode on " + httpAddr)
				fmt.Println()
				return rt.ServeHTTPOnly(httpAddr)
			}
			ui.Note("serving :80 (ACME) + :443 (auto HTTPS)")
			fmt.Println()
			return rt.ServeTLS(certsDir)
		},
	}
	cmd.Flags().StringVar(&domain, "domain", "", "base domain — apps served at <app>.<domain>")
	cmd.Flags().StringVar(&httpAddr, "http-addr", "", "HTTP-only test mode on this address (no TLS)")
	cmd.Flags().StringVar(&panel, "panel", "127.0.0.1:7070", "fallback control panel address for dash.<domain>")
	cmd.Flags().StringVar(&panelPointer, "panel-pointer", "", "file holding the live panel address (default ~/.skiff/panel.addr)")
	cmd.Flags().StringVar(&siteApp, "site-app", "www", "app that serves the apex + www.<domain>")
	cmd.Flags().StringVar(&domainsFile, "domains-file", "", "file of custom host→app mappings (default ~/.skiff/domains.json)")
	cmd.Flags().StringVar(&metricsFile, "metrics-file", "", "file to snapshot request metrics to (default ~/.skiff/metrics.json)")
	cmd.Flags().StringVar(&requestsDir, "requests-dir", "", "directory for the per-request log (default ~/.skiff/requests)")
	cmd.Flags().StringVar(&mailFile, "mail-file", "", "file of email forwarding rules (default ~/.skiff/mail.json)")
	cmd.Flags().StringVar(&smtpAddr, "smtp-addr", ":25", "address to receive forwarded email on (empty disables)")
	cmd.Flags().StringVar(&certsDir, "certs-dir", "", "directory for issued certificates (default /var/lib/skiff/certs on Linux, ~/.skiff/certs elsewhere)")
	return cmd
}

// defaultCertsDir keeps certificates where the Linux installer has always put them. Elsewhere (a Mac, where the router runs as the logged-in user) there is no /var/lib to write to.
func defaultCertsDir(goos, home string) string {
	if goos == "linux" {
		return "/var/lib/skiff/certs"
	}
	return filepath.Join(home, ".skiff", "certs")
}
