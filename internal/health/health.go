// Package health decides when a freshly started container is ready for traffic.
package health

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"
)

const DefaultTimeout = 30 * time.Second

// speaksHTTP2 opens plain-text HTTP/2 and checks the server answers with its settings frame. A gRPC server can refuse any request it's sent, so the handshake is the proof. A port that only accepts and hangs up, as docker's proxy does before the app is up, fails it.
func speaksHTTP2(host string, port int) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 2*time.Second)
	if err != nil {
		return false
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	const clientPreface = "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"
	emptySettings := []byte{0, 0, 0, 0x4, 0, 0, 0, 0, 0}
	if _, err := conn.Write(append([]byte(clientPreface), emptySettings...)); err != nil {
		return false
	}
	var header [9]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return false
	}
	const frameSettings = 0x4
	return header[3] == frameSettings
}

// WaitMode checks readiness the way the service asks to be checked: an open port, nothing at all, or HTTP.
func WaitMode(mode, host string, port int, path string, timeout time.Duration) error {
	switch mode {
	case "none":
		return nil
	case "tcp":
		deadline := time.Now().Add(timeout)
		var last error
		for time.Now().Before(deadline) {
			conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 2*time.Second)
			if err == nil {
				// Docker's port proxy accepts before the app listens, so an immediate close means nothing is there yet.
				_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
				_, rerr := conn.Read(make([]byte, 1))
				conn.Close()
				var nerr net.Error
				if rerr == nil || (errors.As(rerr, &nerr) && nerr.Timeout()) {
					return nil
				}
				last = rerr
			} else {
				last = err
			}
			time.Sleep(500 * time.Millisecond)
		}
		return fmt.Errorf("port %d never opened: %v", port, last)
	}
	return Wait(host, port, path, timeout)
}

// Wait polls the app until it's ready: with no path any HTTP reply at / will do (it's listening), including over plain-text HTTP/2, which is all a gRPC server speaks; with a path it must answer 2xx or 3xx.
func Wait(host string, port int, path string, timeout time.Duration) error {
	strict := path != ""
	if !strict {
		path = "/"
	}
	url := fmt.Sprintf("http://%s:%d%s", host, port, path)
	client := &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if !strict || resp.StatusCode < 400 {
				return nil
			}
			last = fmt.Errorf("GET %s answered %d", path, resp.StatusCode)
		} else {
			last = err
			if !strict {
				if speaksHTTP2(host, port) {
					return nil
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	if last != nil {
		return fmt.Errorf("not healthy within %s: %w", timeout, last)
	}
	return fmt.Errorf("not healthy within %s", timeout)
}
