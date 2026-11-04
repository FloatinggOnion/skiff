package registry

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// A CLI deploy to a server must be recorded in the server's own registry: its
// panel and `skiff sync` reconcile containers against that file, so an app known
// only to the deployer's machine looks like an orphan there.

const remoteSentinel = "__SKIFF_REGISTRY_EOF__"

// remoteBody prints apps.json and a sentinel line, then replaces the file with
// stdin. An empty stdin leaves the file alone, so an aborted update can't wipe it.
const remoteBody = `f="$HOME/.skiff/apps.json"; ` +
	`[ -f "$f" ] && cat "$f"; ` +
	`printf "\n%s\n" ` + remoteSentinel + `; ` +
	`cat > "$f.tmp" && if [ -s "$f.tmp" ]; then mv -f "$f.tmp" "$f"; else rm -f "$f.tmp"; fi`

// remoteScript runs remoteBody under the same apps.lock the server-side Update uses.
const remoteScript = `d="$HOME/.skiff"; mkdir -p "$d" || exit 1; body='` + remoteBody + `'; ` +
	`if command -v flock >/dev/null 2>&1; then exec flock "$d/apps.lock" sh -c "$body"; ` +
	`else exec sh -c "$body"; fi`

// sshCommand is swapped out in tests to run the script against a local $HOME.
var sshCommand = func(target, script string) *exec.Cmd {
	return exec.Command("ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=30", "-T", "--", target,
		"sh -c "+shQuote(script))
}

// PutRemote records a in the registry on sshTarget, as an app local to that host.
func PutRemote(sshTarget string, a App) error {
	a.Host = ""
	return updateRemote(sshTarget, func(apps map[string]App) { apps[a.Name] = a })
}

// DeleteRemote removes name from the registry on sshTarget.
func DeleteRemote(sshTarget, name string) error {
	return updateRemote(sshTarget, func(apps map[string]App) { delete(apps, name) })
}

func updateRemote(sshTarget string, fn func(apps map[string]App)) error {
	cmd := sshCommand(sshTarget, remoteScript)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("updating the registry on %s: %w", sshTarget, err)
	}

	apps, opErr := readRemote(bufio.NewReader(stdout))
	var data []byte
	if opErr == nil {
		fn(apps)
		data, opErr = json.MarshalIndent(apps, "", "  ")
	}
	if opErr == nil {
		_, opErr = stdin.Write(data)
	}
	// Closing without writing leaves the remote file untouched (see remoteBody).
	stdin.Close()
	_, _ = io.Copy(io.Discard, stdout)
	waitErr := cmd.Wait()

	if opErr == nil {
		opErr = waitErr
	}
	if opErr != nil {
		if s := strings.TrimSpace(stderr.String()); s != "" {
			return fmt.Errorf("updating the registry on %s: %w (%s)", sshTarget, opErr, s)
		}
		return fmt.Errorf("updating the registry on %s: %w", sshTarget, opErr)
	}
	return nil
}

func readRemote(r *bufio.Reader) (map[string]App, error) {
	var buf strings.Builder
	for {
		line, err := r.ReadString('\n')
		if strings.TrimRight(line, "\r\n") == remoteSentinel {
			break
		}
		buf.WriteString(line)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, errors.New("server closed the connection before sending its registry")
			}
			return nil, err
		}
	}
	apps := map[string]App{}
	if data := strings.TrimSpace(buf.String()); data != "" {
		if err := json.Unmarshal([]byte(data), &apps); err != nil {
			return nil, fmt.Errorf("server registry is not valid JSON: %w", err)
		}
	}
	return apps, nil
}

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
