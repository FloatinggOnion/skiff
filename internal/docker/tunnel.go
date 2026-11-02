package docker

import (
	"context"
	"io"
	"net"
	"os/exec"
	"sync"
	"time"
)

// DialContainer opens a connection to an address as seen from inside a container, through `docker exec` and the nc in its image. It reaches services that publish no port, and works where the host can't route to container IPs.
func (e *Engine) DialContainer(ctx context.Context, container, host, port string) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Not tied to ctx, which only covers the dial; Close ends the process.
	cmd := e.command("exec", "-i", container, "nc", host, port)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &execConn{cmd: cmd, in: stdin, out: stdout}, nil
}

type execConn struct {
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  io.ReadCloser
	once sync.Once
}

func (c *execConn) Read(b []byte) (int, error)  { return c.out.Read(b) }
func (c *execConn) Write(b []byte) (int, error) { return c.in.Write(b) }

func (c *execConn) Close() error {
	c.once.Do(func() {
		_ = c.in.Close()
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
	})
	return nil
}

type execAddr struct{}

func (execAddr) Network() string { return "docker-exec" }
func (execAddr) String() string  { return "docker-exec" }

func (c *execConn) LocalAddr() net.Addr              { return execAddr{} }
func (c *execConn) RemoteAddr() net.Addr             { return execAddr{} }
func (c *execConn) SetDeadline(time.Time) error      { return nil }
func (c *execConn) SetReadDeadline(time.Time) error  { return nil }
func (c *execConn) SetWriteDeadline(time.Time) error { return nil }
