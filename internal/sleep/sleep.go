// Package sleep is the shared list of apps stopped for being idle: the panel writes it, the router reads it to wake them.
package sleep

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Apps maps each sleeping app to the containers to start when it's next visited.
type Apps map[string][]string

func DefaultFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".skiff", "sleeping.json")
}

func Load(path string) Apps {
	out := Apps{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

func Save(path string, apps Apps) error {
	b, err := json.Marshal(apps)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
