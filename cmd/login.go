package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/nuelScript/skiff/internal/config"
	"github.com/nuelScript/skiff/internal/docker"
	"github.com/nuelScript/skiff/internal/ui"
	"github.com/spf13/cobra"
)

type credentials struct {
	URL   string `json:"url"`
	Token string `json:"token"`
	Team  string `json:"team,omitempty"`
}

func credentialsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".skiff", "credentials.json")
}

func loadCredentials() (credentials, bool) {
	var c credentials
	b, err := os.ReadFile(credentialsPath())
	if err != nil || json.Unmarshal(b, &c) != nil || c.URL == "" || c.Token == "" {
		return credentials{}, false
	}
	return c, true
}

func newLoginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login [dashboard-url]",
		Short: "Sign the CLI in to your Skiff dashboard",
		Long: `Opens your dashboard to approve this machine. Once signed in, deploys to
that server show up under your team with who shipped them, and pick up the
env vars set in the dashboard (skiff.toml still wins when both set a key).`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			base, err := loginBase(args)
			if err != nil {
				return err
			}
			if strings.HasPrefix(base, "http://") && !isLocalURL(base) {
				ui.Note("warning: " + base + " is plain http, so the sign-in and the token it returns travel unencrypted. Use https:// unless this is a network you trust.")
			}
			host, _ := os.Hostname()
			body := strings.NewReader(fmt.Sprintf(`{"host":%q}`, host))
			resp, err := http.Post(base+"/api/cli/login", "application/json", body)
			if err != nil {
				return err
			}
			var start struct{ Code, Poll, ApproveURL string }
			err = json.NewDecoder(resp.Body).Decode(&start)
			resp.Body.Close()
			if err != nil || start.Poll == "" {
				return fmt.Errorf("%s doesn't look like a Skiff dashboard", base)
			}

			fmt.Println()
			ui.Field("code", start.Code)
			ui.Note("open this in your browser and enter the code: " + stripControl(start.ApproveURL))
			if approveURLOK(start.ApproveURL, base) {
				openBrowser(start.ApproveURL)
			}

			deadline := time.Now().Add(10 * time.Minute)
			for time.Now().Before(deadline) {
				time.Sleep(2 * time.Second)
				resp, err := http.Get(base + "/api/cli/login?poll=" + url.QueryEscape(start.Poll))
				if err != nil {
					continue
				}
				switch resp.StatusCode {
				case http.StatusAccepted:
					resp.Body.Close()
					continue
				case http.StatusOK:
					var got struct{ Token, Team string }
					_ = json.NewDecoder(resp.Body).Decode(&got)
					resp.Body.Close()
					if err := saveCredentials(credentials{URL: base, Token: got.Token, Team: got.Team}); err != nil {
						return err
					}
					ui.Done("Signed in to " + base)
					return nil
				default:
					resp.Body.Close()
					return fmt.Errorf("sign-in wasn't approved (%s)", resp.Status)
				}
			}
			return fmt.Errorf("timed out waiting for approval")
		},
	}
}

// approveURLOK reports whether the address the server returned is on the dashboard the user named, so a server can't send the browser elsewhere.
func approveURLOK(approve, base string) bool {
	if approve == base || strings.HasPrefix(approve, base+"/") {
		return true
	}
	u, err := url.Parse(base)
	return err == nil && u.Host != "" && strings.HasPrefix(approve, "https://"+u.Host+"/")
}

func isLocalURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// loginBase is the dashboard to sign in to: the one named, or else the one this machine last used.
func loginBase(args []string) (string, error) {
	raw := ""
	if len(args) > 0 {
		raw = args[0]
	} else if c, ok := loadCredentials(); ok {
		raw = c.URL
	}
	base := strings.TrimSuffix(strings.TrimSpace(raw), "/")
	if base == "" {
		return "", errors.New("name your dashboard the first time: skiff login https://dash.example.com")
	}
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "https://" + base
	}
	return base, nil
}

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Forget the dashboard sign-in on this machine",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := os.Remove(credentialsPath()); err != nil && !os.IsNotExist(err) {
				return err
			}
			ui.Done("Signed out. Revoke the token under Settings → API tokens if this machine isn't yours.")
			return nil
		},
	}
}

func newWhoamiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show which dashboard the CLI is signed in to",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, ok := loadCredentials()
			if !ok {
				fmt.Println("Not signed in. Run `skiff login <dashboard-url>`.")
				return nil
			}
			req, _ := http.NewRequest(http.MethodGet, c.URL+"/api/v1/apps", nil)
			req.Header.Set("Authorization", "Bearer "+c.Token)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusUnauthorized {
				return fmt.Errorf("the saved token was revoked; run `skiff login %s` again", c.URL)
			}
			var apps []struct{ Name string }
			_ = json.NewDecoder(resp.Body).Decode(&apps)
			ui.Field("dashboard", c.URL)
			ui.Field("projects", fmt.Sprint(len(apps)))
			return nil
		},
	}
}

func saveCredentials(c credentials) error {
	path := credentialsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(path, b, 0o600)
}

func openBrowser(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	_ = cmd.Start()
}

// panelFor returns the signed-in dashboard when it runs on the server this config deploys to; deploys made by the panel itself are left alone.
func panelFor(cfg *config.Config) (credentials, bool) {
	if os.Getenv("SKIFF_DEPLOY_ID") != "" || cfg.RemoteHost() == "" {
		return credentials{}, false
	}
	c, ok := loadCredentials()
	if !ok {
		return credentials{}, false
	}
	u, err := url.Parse(c.URL)
	if err != nil {
		return credentials{}, false
	}
	return c, sameMachine(docker.SSHHostname(cfg.RemoteHost()), u.Hostname())
}

func sameMachine(a, b string) bool {
	ips := func(h string) map[string]bool {
		out := map[string]bool{}
		if ip := net.ParseIP(h); ip != nil {
			out[ip.String()] = true
			return out
		}
		addrs, _ := net.LookupHost(h)
		for _, a := range addrs {
			out[a] = true
		}
		return out
	}
	left := ips(a)
	for ip := range ips(b) {
		if left[ip] {
			return true
		}
	}
	return false
}

func (c credentials) call(method, path string, body, out any) (int, error) {
	var rd *strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	} else {
		rd = strings.NewReader("")
	}
	req, err := http.NewRequest(method, c.URL+"/api/v1"+path, rd)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode < 300 {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode, nil
}

// pullDashboardEnv adds the project's dashboard env vars to cfg wherever skiff.toml (or .env) doesn't already set them.
func pullDashboardEnv(c credentials, cfg *config.Config, contextDir string) int {
	var vars []struct {
		Key, Value string
		Build      bool
	}
	if code, err := c.call(http.MethodGet, "/apps/"+url.PathEscape(cfg.Name)+"/env", nil, &vars); err != nil || code != http.StatusOK {
		return 0
	}
	local := cfg.Environment(contextDir)
	if cfg.Env == nil {
		cfg.Env = map[string]string{}
	}
	if cfg.Secrets == nil {
		cfg.Secrets = map[string]string{}
	}
	added := 0
	for _, v := range vars {
		if _, ok := local[v.Key]; ok {
			continue
		}
		if _, ok := cfg.Secrets[v.Key]; ok {
			continue
		}
		if v.Build {
			cfg.Env[v.Key] = v.Value
		} else {
			cfg.Secrets[v.Key] = v.Value
		}
		added++
	}
	return added
}

func reportDeploy(c credentials, cfg *config.Config, id string, labels map[string]string) {
	code, err := c.call(http.MethodPost, "/cli/deploys", map[string]any{
		"app": cfg.Name, "id": id, "commit": labels["skiff.commit"], "message": labels["skiff.message"], "port": cfg.Build.Port,
	}, nil)
	switch {
	case err != nil:
		ui.Note("couldn't record this deploy in the dashboard: " + err.Error())
	case code == http.StatusConflict:
		ui.Note("an app with this name belongs to another team in the dashboard")
	case code == http.StatusUnauthorized:
		ui.Note("the dashboard sign-in was revoked; run `skiff login` again")
	}
}
