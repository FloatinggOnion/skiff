package docker

import (
	"bufio"
	"context"
	"io"
	"strings"
	"sync"
	"time"
)

func (e *Engine) Logs(container string, follow bool, tail string, out io.Writer) error {
	args := []string{"logs"}
	if follow {
		args = append(args, "--follow")
	}
	if tail != "" {
		args = append(args, "--tail", tail)
	}
	args = append(args, container)
	cmd := e.command(args...)
	cmd.Stdout = out
	cmd.Stderr = out
	return cmd.Run()
}

func (e *Engine) StreamLogs(ctx context.Context, container string, out io.Writer) error {
	cmd := e.contextCommand(ctx, "logs", "--tail", "100", "--follow", container)
	cmd.Stdout = out
	cmd.Stderr = out
	return cmd.Run()
}

// FollowLogs streams a container's output from since onward, calling line for each one with docker's timestamp; it returns when the container stops or ctx ends.
func (e *Engine) FollowLogs(ctx context.Context, container string, since time.Time, line func(ts time.Time, stream, text string)) error {
	if c := e.api(); c != nil {
		return apiFollowLogs(ctx, c, container, since, line)
	}
	args := []string{"logs", "--follow", "--timestamps"}
	if !since.IsZero() {
		args = append(args, "--since", since.UTC().Format(time.RFC3339Nano))
	}
	cmd := e.contextCommand(ctx, append(args, container)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	var mu sync.Mutex
	read := func(r io.Reader, stream string, done chan<- struct{}) {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			stamp, text, _ := strings.Cut(sc.Text(), " ")
			ts, err := time.Parse(time.RFC3339Nano, stamp)
			if err != nil {
				ts, text = time.Now(), sc.Text()
			}
			mu.Lock()
			line(ts, stream, text)
			mu.Unlock()
		}
		close(done)
	}
	outDone, errDone := make(chan struct{}), make(chan struct{})
	go read(stdout, "out", outDone)
	go read(stderr, "err", errDone)
	<-outDone
	<-errDone
	return cmd.Wait()
}
