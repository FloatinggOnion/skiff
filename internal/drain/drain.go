// Package drain is the shared list of containers that shouldn't get new requests: replaced ones still finishing their open requests, which the router retires, and new ones not yet healthy.
package drain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// RouterBeatEvery is how often the router says it's running. A deploy only leaves a container to the router if it has heard from it recently.
const RouterBeatEvery = 5 * time.Second

type Entry struct {
	App       string `json:"app"`
	Container string `json:"container"`
	HostPort  int    `json:"hostPort"`
	Since     int64  `json:"since"`
	Until     int64  `json:"until"`
	// Starting marks a new container held back until its deploy says it's healthy. The deploy removes the entry, or it runs out if the deploy dies.
	Starting bool `json:"starting,omitempty"`
}

func skiffPath(name string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".skiff", name)
}

func Load() []Entry {
	var out []Entry
	if b, err := os.ReadFile(skiffPath("draining.json")); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

// Active names the containers still within their time.
func Active(now time.Time) map[string]bool {
	out := map[string]bool{}
	for _, e := range Load() {
		if now.Unix() < e.Until {
			out[e.Container] = true
		}
	}
	return out
}

func update(fn func([]Entry) []Entry) error {
	path := skiffPath("draining.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	b, err := json.Marshal(fn(Load()))
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func Add(e Entry) error {
	return update(func(list []Entry) []Entry { return append(list, e) })
}

func Remove(container string) error {
	return update(func(list []Entry) []Entry {
		out := list[:0]
		for _, e := range list {
			if e.Container != container {
				out = append(out, e)
			}
		}
		return out
	})
}

// Beat records that a router able to retire containers is running.
func Beat() {
	path := skiffPath("router.alive")
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		_ = os.WriteFile(path, nil, 0o644)
	}
}

func RouterAlive() bool {
	info, err := os.Stat(skiffPath("router.alive"))
	return err == nil && time.Since(info.ModTime()) < 3*RouterBeatEvery
}
