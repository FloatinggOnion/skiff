// Package health decides when a freshly started container is ready for traffic.
package health

import (
	"fmt"
	"net/http"
	"time"
)

const DefaultTimeout = 30 * time.Second

// Wait polls the app until it's ready: with no path any HTTP reply at / will do (it's listening); with a path it must answer 2xx or 3xx.
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
		}
		time.Sleep(500 * time.Millisecond)
	}
	if last != nil {
		return fmt.Errorf("not healthy within %s: %w", timeout, last)
	}
	return fmt.Errorf("not healthy within %s", timeout)
}
