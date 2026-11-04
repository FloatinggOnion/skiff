package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"github.com/nuelScript/skiff/internal/docker"
	"github.com/nuelScript/skiff/internal/registry"
	"github.com/nuelScript/skiff/internal/ui"
	"github.com/spf13/cobra"
)

func newLogsCmd() *cobra.Command {
	var follow bool
	var tail string
	cmd := &cobra.Command{
		Use:   "logs <app>",
		Short: "Show logs for a deployed app",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			apps, err := registry.Load()
			if err != nil {
				return err
			}
			app, ok := apps[name]
			if ok && docker.For(app.Host).State(app.Container) != "missing" {
				return docker.For(app.Host).Logs(app.Container, follow, tail, termSafe(os.Stdout))
			}
			if creds, signedIn := loadCredentials(); signedIn {
				return remoteLogs(creds, name, follow, tail, termSafe(os.Stdout))
			}
			ui.Fail("No app named " + name + " — run `skiff ls`, or `skiff login` to read logs from a server")
			return fmt.Errorf("unknown app %q", name)
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "stream new logs as they arrive")
	cmd.Flags().StringVarP(&tail, "tail", "n", "100", "number of recent lines to show first")
	return cmd
}

// remoteLogs reads an app's logs from the dashboard the CLI is signed in to.
func remoteLogs(c credentials, name string, follow bool, tail string, out io.Writer) error {
	q := url.Values{}
	if n, err := strconv.Atoi(tail); err == nil && n > 0 {
		q.Set("lines", tail)
	}
	if follow {
		q.Set("follow", "1")
	}
	req, err := http.NewRequest(http.MethodGet, c.URL+"/api/v1/apps/"+url.PathEscape(name)+"/logs?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return fmt.Errorf("the saved token was revoked; run `skiff login %s` again", c.URL)
	case http.StatusNotFound:
		return fmt.Errorf("no running app named %q on %s", name, c.URL)
	default:
		return fmt.Errorf("%s answered %s", c.URL, resp.Status)
	}
	if follow {
		_, err = io.Copy(out, resp.Body)
		return err
	}
	var body struct {
		Logs string `json:"logs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return err
	}
	_, err = io.WriteString(out, body.Logs)
	return err
}
