package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/coder/websocket"
	"github.com/nuelScript/skiff/internal/ui"
	"github.com/spf13/cobra"
)

const (
	tunnelPingEvery   = 30 * time.Second
	tunnelPingTimeout = 90 * time.Second
	tunnelMaxMessage  = 1 << 20
)

type remoteDatabase struct {
	Name   string `json:"name"`
	Engine string `json:"engine"`
	State  string `json:"state"`
	Port   int    `json:"port"`
}

func newDBCmd() *cobra.Command {
	db := &cobra.Command{Use: "db", Short: "Reach your team's databases"}
	db.AddCommand(&cobra.Command{
		Use:   "ls",
		Short: "List your team's databases",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := signedIn()
			if err != nil {
				return err
			}
			dbs, err := listRemoteDatabases(c)
			if err != nil {
				return err
			}
			if len(dbs) == 0 {
				ui.Note("No databases yet")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tENGINE\tSTATE")
			for _, d := range dbs {
				fmt.Fprintf(w, "%s\t%s\t%s\n", d.Name, d.Engine, d.State)
			}
			return w.Flush()
		},
	})
	var port int
	connect := &cobra.Command{
		Use:   "connect <name>",
		Short: "Open a private tunnel to a database for local clients",
		Long: `Listens on this machine and carries connections to the database over your
dashboard's address. The database stays closed to the internet.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := signedIn()
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return connectDatabase(ctx, c, args[0], port)
		},
	}
	connect.Flags().IntVarP(&port, "port", "p", 0, "local port (default: the engine's usual port, or any free one)")
	db.AddCommand(connect)
	return db
}

func signedIn() (credentials, error) {
	c, ok := loadCredentials()
	if !ok {
		return credentials{}, errors.New("not signed in, run `skiff login <dashboard-url>` first")
	}
	return c, nil
}

func listRemoteDatabases(c credentials) ([]remoteDatabase, error) {
	var dbs []remoteDatabase
	code, err := c.call(http.MethodGet, "/databases", nil, &dbs)
	if err != nil {
		return nil, err
	}
	switch {
	case code == http.StatusUnauthorized:
		return nil, errors.New("your sign-in is no longer valid, run `skiff login` again")
	case code == http.StatusNotFound, code == http.StatusOK && dbs == nil:
		// An older server answers this path with its dashboard page, which isn't a list.
		return nil, errors.New("this server is too old for that, update Skiff on it first")
	case code == http.StatusOK:
		return dbs, nil
	}
	return nil, fmt.Errorf("the dashboard answered %d", code)
}

// listenLocal only ever binds loopback. A port the user chose must be free; otherwise it falls back to any free one.
func listenLocal(chosen, usual int) (net.Listener, error) {
	if chosen != 0 {
		return net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(chosen)))
	}
	if l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(usual))); err == nil {
		return l, nil
	}
	return net.Listen("tcp", "127.0.0.1:0")
}

func connectDatabase(ctx context.Context, c credentials, name string, port int) error {
	dbs, err := listRemoteDatabases(c)
	if err != nil {
		return err
	}
	var db *remoteDatabase
	for i := range dbs {
		if dbs[i].Name == name {
			db = &dbs[i]
			break
		}
	}
	if db == nil {
		return fmt.Errorf("no database named %s, run `skiff db ls` to see yours", name)
	}
	if db.State != "running" {
		return fmt.Errorf("%s is %s, so there's nothing to connect to", name, db.State)
	}
	l, err := listenLocal(port, db.Port)
	if err != nil {
		return err
	}
	defer l.Close()
	localPort := l.Addr().(*net.TCPAddr).Port

	var conn struct {
		URL string `json:"url"`
	}
	code, err := c.call(http.MethodGet, "/databases/"+url.PathEscape(name)+"/connection?port="+strconv.Itoa(localPort), nil, &conn)
	if err != nil {
		return err
	}
	if code == http.StatusForbidden {
		return errors.New("this sign-in can't reach databases, run `skiff login` again")
	}
	if code != http.StatusOK || conn.URL == "" {
		return fmt.Errorf("couldn't read the address of %s (the dashboard answered %d)", name, code)
	}
	ui.Done("Tunnel open to " + name)
	ui.Field("address", conn.URL)
	if strings.HasPrefix(c.URL, "http://") {
		ui.Note("Your dashboard is on plain HTTP, so this traffic isn't encrypted on its way to the server.")
	}
	ui.Note("Press Ctrl-C to close it")
	serveTunnel(ctx, l, c, name, os.Stderr)
	return nil
}

// serveTunnel carries each local connection through its own WebSocket until ctx ends, then waits for them to close.
func serveTunnel(ctx context.Context, l net.Listener, c credentials, name string, errs io.Writer) {
	go func() {
		<-ctx.Done()
		_ = l.Close()
	}()
	var wg sync.WaitGroup
	defer wg.Wait()
	target := strings.Replace(c.URL, "http", "ws", 1) + "/api/v1/databases/" + url.PathEscape(name) + "/tunnel"
	for {
		local, err := l.Accept()
		if err != nil {
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer local.Close()
			if err := carryConnection(ctx, local, target, c.Token); err != nil && ctx.Err() == nil {
				fmt.Fprintln(errs, "skiff: connection to "+name+" failed: "+err.Error())
			}
		}()
	}
}

func carryConnection(parent context.Context, local net.Conn, target, token string) error {
	dialCtx, cancelDial := context.WithTimeout(parent, 20*time.Second)
	defer cancelDial()
	ws, resp, err := websocket.Dial(dialCtx, target, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + token}},
	})
	if err != nil {
		return tunnelDialError(resp, err)
	}
	defer ws.CloseNow()
	ws.SetReadLimit(tunnelMaxMessage)

	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	remote := websocket.NetConn(ctx, ws, websocket.MessageBinary)
	go func() {
		_, _ = io.Copy(remote, local)
		cancel()
	}()
	go func() {
		_, _ = io.Copy(local, remote)
		cancel()
	}()
	go func() {
		<-ctx.Done()
		_ = local.Close()
	}()
	tick := time.NewTicker(tunnelPingEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			pingCtx, stop := context.WithTimeout(ctx, tunnelPingTimeout)
			err := ws.Ping(pingCtx)
			stop()
			if err != nil && ctx.Err() == nil {
				return errors.New("lost the connection to the server")
			}
		}
	}
}

func tunnelDialError(resp *http.Response, err error) error {
	if resp == nil || resp.Body == nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return errors.New("your sign-in is no longer valid, run `skiff login` again")
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if msg := apiErrorMessage(body); msg != "" {
		return errors.New(msg)
	}
	return err
}

func apiErrorMessage(body []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &e) != nil {
		return ""
	}
	return e.Error
}
