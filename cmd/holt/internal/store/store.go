// Package store is the hub's durable non-secret state: a single SQLite
// database under the config directory (~/.holt/holt.db). It exposes
// its *sql.DB so the tunnel presence directory and the peer denylist
// share the same file (each owns and migrates its own table). The
// hub's JWT secret lives as a file next to it (see the hubsecret
// package).
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, registered as "sqlite"
)

// Store owns the SQLite connection and the hub's persistent tables.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the hub database under dir and
// migrates the schema.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}

	dsn := "file:" + filepath.Join(dir, "holt.db") + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}

	return &Store{db: db}, nil
}

// DB returns the underlying handle so other components (e.g. sqldir)
// can share the same database file.
func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) Close() error { return s.db.Close() }
