// Package sqldir is a SQL-backed directory.Directory: a shared peer-presence
// store so a fleet of hubs can each see who is attached where. It
// works on SQLite and PostgreSQL.
//
// It imports only database/sql — the consumer brings the driver
// (modernc.org/sqlite, jackc/pgx/stdlib, lib/pq, …) and hands in an
// opened *sql.DB. That keeps the holt module free of any driver
// dependency.
//
// Presence only: this stores which hub owns a peer's live tunnel, the
// peer's version, and when it attached. The live *http2.ClientConn is
// never stored — only the owning hub can dial a peer.
package sqldir

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/openotters/holt/internal/directory"
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

// Directory is a SQL-backed directory.Directory.
type Directory struct {
	db      *sql.DB
	dialect Dialect
	table   string
}

// Option configures a Directory.
type Option func(*Directory)

// WithTable overrides the table name (default "holt_peers").
func WithTable(name string) Option {
	return func(d *Directory) { d.table = name }
}

// New returns a SQL directory over db. Call Migrate once to create the
// table. db must be opened with a driver the consumer imports.
func New(db *sql.DB, dialect Dialect, opts ...Option) *Directory {
	d := &Directory{db: db, dialect: dialect, table: "holt_peers"}
	for _, opt := range opts {
		opt(d)
	}

	return d
}

// Migrate creates the presence table if it does not exist. Idempotent.
func (d *Directory) Migrate(ctx context.Context) error {
	stmt := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
	peer         TEXT PRIMARY KEY,
	hub_id       TEXT NOT NULL,
	peer_version TEXT NOT NULL,
	attached_at  BIGINT NOT NULL
)`, d.table)

	if _, err := d.db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("sqldir: migrate: %w", err)
	}

	return nil
}

// Attach upserts the peer's presence row.
func (d *Directory) Attach(ctx context.Context, rec directory.PeerRecord) error {
	// ON CONFLICT ... DO UPDATE is supported by both SQLite and
	// PostgreSQL; only the placeholders differ.
	q := d.rebind(fmt.Sprintf(
		`INSERT INTO %s (peer, hub_id, peer_version, attached_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT (peer) DO UPDATE SET hub_id = excluded.hub_id,
		   peer_version = excluded.peer_version, attached_at = excluded.attached_at`,
		d.table))

	if _, err := d.db.ExecContext(ctx, q,
		rec.Peer, rec.Hub, rec.PeerVersion, rec.AttachedAt.UnixNano()); err != nil {
		return fmt.Errorf("sqldir: attach %s: %w", rec.Peer, err)
	}

	return nil
}

// Detach removes the peer's row only if hub still owns it.
func (d *Directory) Detach(ctx context.Context, peer, hubID string) error {
	q := d.rebind(fmt.Sprintf(`DELETE FROM %s WHERE peer = ? AND hub_id = ?`, d.table))

	if _, err := d.db.ExecContext(ctx, q, peer, hubID); err != nil {
		return fmt.Errorf("sqldir: detach %s: %w", peer, err)
	}

	return nil
}

// Lookup returns the peer's record.
func (d *Directory) Lookup(ctx context.Context, peer string) (directory.PeerRecord, bool, error) {
	q := d.rebind(fmt.Sprintf(
		`SELECT hub_id, peer_version, attached_at FROM %s WHERE peer = ?`, d.table))

	var (
		hubID   string
		version string
		nanos   int64
	)

	scanErr := d.db.QueryRowContext(ctx, q, peer).Scan(&hubID, &version, &nanos)
	if scanErr != nil {
		if errors.Is(scanErr, sql.ErrNoRows) {
			return directory.PeerRecord{}, false, nil
		}

		return directory.PeerRecord{}, false, fmt.Errorf("sqldir: lookup %s: %w", peer, scanErr)
	}

	return directory.PeerRecord{
		Peer:        peer,
		Hub:         hubID,
		PeerVersion: version,
		AttachedAt:  time.Unix(0, nanos),
	}, true, nil
}

// List returns every attached peer, ordered by peer id.
func (d *Directory) List(ctx context.Context) ([]directory.PeerRecord, error) {
	//nolint:gosec // G201: table is a caller-controlled identifier (WithTable / default), never user input.
	q := fmt.Sprintf(
		`SELECT peer, hub_id, peer_version, attached_at FROM %s ORDER BY peer`, d.table)

	rows, err := d.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("sqldir: list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []directory.PeerRecord

	for rows.Next() {
		var (
			peer, hubID, version string
			nanos                int64
		)

		if scanErr := rows.Scan(&peer, &hubID, &version, &nanos); scanErr != nil {
			return nil, fmt.Errorf("sqldir: list scan: %w", scanErr)
		}

		out = append(out, directory.PeerRecord{
			Peer: peer, Hub: hubID, PeerVersion: version, AttachedAt: time.Unix(0, nanos),
		})
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("sqldir: list rows: %w", rowsErr)
	}

	return out, nil
}

// ClearHub removes every row owned by hubID (boot-time stale cleanup).
func (d *Directory) ClearHub(ctx context.Context, hubID string) error {
	q := d.rebind(fmt.Sprintf(`DELETE FROM %s WHERE hub_id = ?`, d.table))

	if _, err := d.db.ExecContext(ctx, q, hubID); err != nil {
		return fmt.Errorf("sqldir: clear hub %s: %w", hubID, err)
	}

	return nil
}

// rebind rewrites ? placeholders to $1, $2, … for PostgreSQL; SQLite
// keeps ?. Kept trivially simple because every query here has a small,
// fixed number of parameters and no literal ? in the SQL text.
func (d *Directory) rebind(q string) string {
	if d.dialect != Postgres {
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

var _ directory.Directory = (*Directory)(nil)
