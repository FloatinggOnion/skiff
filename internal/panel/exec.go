package panel

import (
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"

	"github.com/coder/websocket"
	"github.com/creack/pty"

	"github.com/nuelScript/skiff/internal/registry"
)

func (p *Panel) handleExec(w http.ResponseWriter, r *http.Request) {
	app := sanitizeName(r.URL.Query().Get("app"))
	if !p.canAccess(r, app) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	apps, err := registry.Load()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a, ok := apps[app]
	if !ok {
		http.Error(w, "unknown app", http.StatusNotFound)
		return
	}
	if a.Host != "" {
		http.Error(w, "console isn't available for remote apps yet", http.StatusBadRequest)
		return
	}
	p.serveContainerShell(w, r, a.Container,
		[]string{"sh", "-c", "command -v bash >/dev/null 2>&1 && exec bash || exec sh"})
}

func (p *Panel) serveContainerShell(w http.ResponseWriter, r *http.Request, container string, shellArgs []string) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()

	// r.Context() is canceled once Accept hijacks the connection, so give the session its own context, canceled when either side goes away.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Images without a shell (distroless ones, scratch builds) are normal; say so rather than showing docker's error.
	if out, err := exec.CommandContext(ctx, "docker", "exec", container, "sh", "-c", "exit 0").CombinedOutput(); err != nil && strings.Contains(string(out), "executable file not found") {
		_ = c.Write(ctx, websocket.MessageText, []byte("\x1b[33mThis image has no shell, so there's nothing to open a console in.\x1b[0m\r\nThe app itself is fine. Images built without one, like distroless images, can't use the console.\r\n"))
		return
	}

	cmd := exec.CommandContext(ctx, "docker", append([]string{"exec", "-it", container}, shellArgs...)...)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		_ = c.Write(ctx, websocket.MessageText, []byte("\r\n\x1b[31mcouldn't open a shell in this container\x1b[0m\r\n"))
		return
	}
	defer func() { _ = ptmx.Close() }()

	go func() {
		buf := make([]byte, 8192)
		for {
			n, rerr := ptmx.Read(buf)
			if n > 0 {
				if werr := c.Write(ctx, websocket.MessageBinary, buf[:n]); werr != nil {
					cancel()
					return
				}
			}
			if rerr != nil {
				cancel()
				return
			}
		}
	}()

	var msg struct {
		T string `json:"t"`
		D string `json:"d"`
		C int    `json:"c"`
		R int    `json:"r"`
	}
	for {
		_, data, rerr := c.Read(ctx)
		if rerr != nil {
			return
		}
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		switch msg.T {
		case "in":
			_, _ = ptmx.WriteString(msg.D)
		case "resize":
			// Clamp browser-supplied dims before the uint16 cast so a bogus value can't silently wrap to a tiny terminal.
			if msg.C > 0 && msg.R > 0 && msg.C <= 1000 && msg.R <= 1000 {
				_ = pty.Setsize(ptmx, &pty.Winsize{Rows: uint16(msg.R), Cols: uint16(msg.C)})
			}
		}
	}
}
