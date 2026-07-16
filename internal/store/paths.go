// Package store persists cormake's workspaces and tasks as flat JSON files
// on the local filesystem, per VISION.md §6/§7 — it owns reading/writing
// that state and knows nothing about the UI or Claude.
package store

import (
	"os"
	"path/filepath"
)

// AppDir resolves the directory cormake stores its state under: ~/.cormake,
// or $CORMAKE_HOME if set (non-empty), which exists so tests and manual
// verification can point at a scratch directory instead of the real one.
func AppDir() (string, error) {
	if dir := os.Getenv("CORMAKE_HOME"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cormake"), nil
}
