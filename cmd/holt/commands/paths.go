package commands

import (
	"os"
	"path/filepath"
	"strings"
)

// defaultStateDir is where the hub keeps its cert + JWT secret and its
// SQLite database — ~/.holt.
func defaultStateDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".holt")
	}

	return filepath.Join(os.TempDir(), "holt-state")
}

// tildePath shortens a path under the home directory to ~/... for
// display; storage always uses the full path.
func tildePath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}

	if rel, relErr := filepath.Rel(home, p); relErr == nil && !strings.HasPrefix(rel, "..") {
		return filepath.Join("~", rel)
	}

	return p
}
