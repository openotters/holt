package commands

import (
	"os"
	"path/filepath"
)

// defaultStateDir is where the hub keeps its cert + JWT secret and its
// SQLite database — ~/.holt.
func defaultStateDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".holt")
	}

	return filepath.Join(os.TempDir(), "holt-state")
}
