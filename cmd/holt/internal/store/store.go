// Package store is the hub's durable non-secret state: a single SQLite
// database under the config directory (~/.holt/holt.db). It holds
// the peer blocklist and exposes its *sql.DB so the tunnel
// presence Directory (hub/sqldir) shares the same file. The hub's JWT
// secret lives as a file next to it (see the hubsecret package).
package store

import (
	"context"
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

	s := &Store{db: db}
	if migErr := s.migrate(context.Background()); migErr != nil {
		_ = db.Close()

		return nil, migErr
	}

	return s, nil
}

// DB returns the underlying handle so other components (e.g. sqldir)
// can share the same database file.
func (s *Store) DB() *sql.DB { return s.db }

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS blocked_peers (
	peer       TEXT PRIMARY KEY,
	blocked_at INTEGER NOT NULL
);`

	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}

	return nil
}

// LoadBlocked returns the currently-blocked peers keyed by peer id,
// valued by the unix time they were blocked.
func (s *Store) LoadBlocked() (map[string]int64, error) {
	rows, err := s.db.QueryContext(context.Background(), `SELECT peer, blocked_at FROM blocked_peers`)
	if err != nil {
		return nil, fmt.Errorf("store: load blocked: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string]int64)

	for rows.Next() {
		var (
			peer    string
			blocked int64
		)
		if scanErr := rows.Scan(&peer, &blocked); scanErr != nil {
			return nil, scanErr
		}

		out[peer] = blocked
	}

	return out, rows.Err()
}

// Block records a blocked peer (unix seconds).
func (s *Store) Block(peer string, atUnix int64) error {
	_, err := s.db.ExecContext(context.Background(),
		`INSERT INTO blocked_peers (peer, blocked_at) VALUES (?, ?) ON CONFLICT (peer) DO NOTHING`,
		peer, atUnix)
	if err != nil {
		return fmt.Errorf("store: block %s: %w", peer, err)
	}

	return nil
}

// Unblock removes a peer from the blocklist.
func (s *Store) Unblock(peer string) error {
	_, err := s.db.ExecContext(context.Background(), `DELETE FROM blocked_peers WHERE peer = ?`, peer)
	if err != nil {
		return fmt.Errorf("store: unblock %s: %w", peer, err)
	}

	return nil
}
