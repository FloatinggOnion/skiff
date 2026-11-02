package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/nuelScript/skiff/internal/docker"
	"github.com/nuelScript/skiff/internal/panel"
	"github.com/nuelScript/skiff/internal/ui"
	"github.com/spf13/cobra"
)

func newPanelCmd() *cobra.Command {
	var addr, domain string
	cmd := &cobra.Command{
		Use:   "panel",
		Short: "Run the hosted control panel (deploy + manage from the browser)",
		RunE: func(cmd *cobra.Command, args []string) error {
			pw := os.Getenv("SKIFF_PANEL_PASSWORD")
			if pw == "" {
				return fmt.Errorf("set SKIFF_PANEL_PASSWORD (the setup secret for creating the first account)")
			}
			if domain == "" {
				domain = "localhost"
			}
			pn, err := panel.New(pw, domain, docker.Local())
			if err != nil {
				return fmt.Errorf("start the panel: %w", err)
			}

			ui.Banner(version)
			shown := addr
			if strings.HasPrefix(shown, ":") {
				shown = "localhost" + shown
			}
			ui.Field("panel", "http://"+shown)
			ui.Field("domain", domain)
			ui.Note("control panel — deploy + manage from the browser (ctrl-c to stop)")
			fmt.Println()

			// Graceful shutdown: drain in-flight requests on SIGTERM/SIGINT so a control-plane swap never severs a live deploy stream.
			srv := &http.Server{
				Addr:              addr,
				Handler:           pn.Handler(),
				ReadHeaderTimeout: 10 * time.Second,
				IdleTimeout:       120 * time.Second,
			}
			go func() {
				sig := make(chan os.Signal, 1)
				signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
				<-sig
				ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
				defer cancel()
				_ = srv.Shutdown(ctx)
			}()
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:7070", "listen address (loopback by default — the edge router proxies to it)")
	cmd.Flags().StringVar(&domain, "domain", "", "base domain for app URLs")
	return cmd
}
