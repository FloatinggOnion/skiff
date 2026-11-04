package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"

	"github.com/nuelScript/skiff/internal/docker"
	"github.com/nuelScript/skiff/internal/proxy"
	"github.com/nuelScript/skiff/internal/registry"
	"github.com/nuelScript/skiff/internal/ui"
	"github.com/spf13/cobra"
)

func newOpenCmd() *cobra.Command {
	var printOnly bool
	cmd := &cobra.Command{
		Use:   "open <app>",
		Short: "Open a deployed app's URL in the browser",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			apps, err := registry.Load()
			if err != nil {
				return err
			}
			app, ok := apps[name]
			if !ok {
				ui.Fail("No app named " + name + " — run `skiff ls`")
				return fmt.Errorf("unknown app %q", name)
			}

			url := proxy.URL(app.Name)
			if app.Host != "" && printOnly {
				fmt.Printf("ssh -N -L %d:127.0.0.1:%d %s\n", app.HostPort, app.HostPort, app.Host)
				return nil
			}
			if app.Host != "" {
				// A remote app listens only on its server's loopback: reach it through a tunnel that lasts until this command is stopped.
				port, stop, err := docker.Forward(app.Host, app.HostPort)
				if err != nil {
					ui.Fail(err.Error())
					return err
				}
				defer stop()
				url = fmt.Sprintf("http://127.0.0.1:%d", port)
				defer waitForInterrupt()
			}

			if printOnly {
				fmt.Println(url)
				return nil
			}
			if err := openURL(url); err != nil {
				ui.Fail("Couldn't open the browser")
				ui.Note(url)
				return err
			}
			ui.Done("Opened " + url)
			return nil
		},
	}
	cmd.Flags().BoolVar(&printOnly, "print", false, "print the URL instead of opening a browser")
	return cmd
}

func openURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

func waitForInterrupt() {
	ui.Note("Connected through an SSH tunnel. Press ctrl-c to close it.")
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	<-ch
}
