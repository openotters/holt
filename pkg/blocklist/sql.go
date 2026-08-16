package blocklist

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Dialect selects SQL syntax that differs between engines (parameter
// placeholders, mainly).
type Dialect int

const (
	// SQLite uses ? placeholders.
	SQLite Dialect = iota
	// Postgres uses $1, $2, … placeholders.
	Postgres
)

// SQLStore is the SQL-backed Store. It imports only database/sql —
// the consumer brings the driver and hands in an opened *sql.DB.
type SQLStore struct {
	db      *sql.DB
	dialect Dialect
	table   string
}

// SQLOption configures a SQLStore.
type SQLOption func(*SQLStore)

// WithTable overrides the table name (default "blocked_peers").
func WithTable(name string) SQLOption {
	return func(s *SQLStore) { s.table = name }
}

// NewSQL returns a SQL denylist store over db. Call Migrate once to
// create the table.
func NewSQL(db *sql.DB, dialect Dialect, opts ...SQLOption) *SQLStore {
	s := &SQLStore{db: db, dialect: dialect, table: "blocked_peers"}
	for _, opt := range opts {
		opt(s)
	}

	return s
}

// Migrate creates the denylist table if it does not exist. Idempotent.
func (s *SQLStore) Migrate(ctx context.Context) error {
	stmt := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
	peer       TEXT PRIMARY KEY,
	blocked_at BIGINT NOT NULL
)`, s.table)

	if _, err := s.db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("blocklist: migrate: %w", err)
	}

	return nil
}

// Load returns the currently-blocked peers keyed by peer id, valued
// by the unix time they were blocked.
func (s *SQLStore) Load(ctx context.Context) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`SELECT peer, blocked_at FROM %s`, s.table))
	if err != nil {
		return nil, fmt.Errorf("blocklist: load: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string]int64)

	for rows.Next() {
		var (
			peer string
			at   int64
		)
		if scanErr := rows.Scan(&peer, &at); scanErr != nil {
			return nil, scanErr
		}

		out[peer] = at
	}

	return out, rows.Err()
}

// IsBlocked reports whether peer has a denylist row.
func (s *SQLStore) IsBlocked(ctx context.Context, peer string) (bool, error) {
	query := s.rebind(fmt.Sprintf(`SELECT 1 FROM %s WHERE peer = ?`, s.table))

	var one int

	err := s.db.QueryRowContext(ctx, query, peer).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("blocklist: is-blocked %s: %w", peer, err)
	}

	return true, nil
}

// Block records a blocked peer (unix seconds), keeping the original
// block time if the peer is already blocked.
func (s *SQLStore) Block(ctx context.Context, peer string, atUnix int64) error {
	stmt := s.rebind(fmt.Sprintf(`INSERT INTO %s (peer, blocked_at) VALUES (?, ?) ON CONFLICT (peer) DO NOTHING`,
		s.table))

	if _, err := s.db.ExecContext(ctx, stmt, peer, atUnix); err != nil {
		return fmt.Errorf("blocklist: block %s: %w", peer, err)
	}

	return nil
}

// Unblock removes a peer from the denylist.
func (s *SQLStore) Unblock(ctx context.Context, peer string) error {
	stmt := s.rebind(fmt.Sprintf(`DELETE FROM %s WHERE peer = ?`, s.table))

	if _, err := s.db.ExecContext(ctx, stmt, peer); err != nil {
		return fmt.Errorf("blocklist: unblock %s: %w", peer, err)
	}

	return nil
}

// rebind rewrites ? placeholders to $1, $2, … for PostgreSQL; SQLite
// keeps ?. Kept trivially simple because every query here has a small,
// fixed number of parameters and no literal ? in the SQL text.
func (s *SQLStore) rebind(q string) string {
	if s.dialect != Postgres {
		return q
	}

	var b strings.Builder

	n := 0

	for _, r := range q {
		if r == '?' {
			n++
			_, _ = fmt.Fprintf(&b, "$%d", n)

			continue
		}

		b.WriteRune(r)
	}

	return b.String()
}

var _ Store = (*SQLStore)(nil)
