// Package edge is the shared list of per-app rules at the router: the panel writes it, the router reads it live.
package edge

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Rule struct {
	// Private means no public address: the router answers as if the app didn't exist.
	Private bool `json:"private,omitempty"`
	// RateLimit is requests per minute per visitor; 0 means none.
	RateLimit int `json:"rateLimit,omitempty"`
}

type Rules map[string]Rule

func DefaultFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".skiff", "edge.json")
}

func Load(path string) Rules {
	out := Rules{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

func Save(path string, rules Rules) error {
	b, err := json.Marshal(rules)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
