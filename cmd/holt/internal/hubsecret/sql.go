package hubsecret

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Dialect selects SQL syntax that differs between engines (parameter
// placeholders and the bytes column type).
type Dialect int

const (
	// SQLite uses ? placeholders and BLOB.
	SQLite Dialect = iota
	// Postgres uses $1, $2, … placeholders and BYTEA.
	Postgres
)

// secretRow is the single row the table holds; the hub's identity is
// one value, not a set, so the key is a constant.
const secretRow = "hub"

// SQLStore keeps the signing secret in the hub's SQL backend, so a
// fleet sharing a database shares one identity: every hub verifies the
// tokens every other hub mints, and a rotation reaches all of them.
// It imports only database/sql — the consumer brings the driver.
type SQLStore struct {
	db      *sql.DB
	dialect Dialect
	table   string
	seed    []byte // adopted on create, see WithSeed
}

// SQLOption configures a SQLStore.
type SQLOption func(*SQLStore)

// WithTable overrides the table name (default "holt_hub_secret").
func WithTable(name string) SQLOption {
	return func(s *SQLStore) { s.table = name }
}

// WithSeed supplies the value to adopt when the table has no secret
// yet — the hub's existing file secret, so moving identity into a
// shared database keeps every token already handed out valid. Ignored
// once a secret exists.
func WithSeed(secret []byte) SQLOption {
	return func(s *SQLStore) { s.seed = secret }
}

// NewSQL returns a SQL-backed store over db. Call Migrate once to
// create the table.
func NewSQL(db *sql.DB, dialect Dialect, opts ...SQLOption) *SQLStore {
	s := &SQLStore{db: db, dialect: dialect, table: "holt_hub_secret"}
	for _, opt := range opts {
		opt(s)
	}

	return s
}

// Describe names the backend for operator messages.
func (s *SQLStore) Describe() string { return "the shared database (" + s.table + ")" }

// Migrate creates the secret table if it does not exist. Idempotent.
func (s *SQLStore) Migrate(ctx context.Context) error {
	stmt := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
	id         TEXT PRIMARY KEY,
	secret     %s NOT NULL,
	updated_at BIGINT NOT NULL
)`, s.table, s.bytesColumn())

	if _, err := s.db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("hubsecret: migrate: %w", err)
	}

	return nil
}

// LoadOrCreate returns the shared secret, creating it on first use.
//
// The insert is conditional and the read follows it, so hubs starting
// at the same moment converge on one secret: whoever loses the race
// inserts nothing and reads the winner's value. Minting a second
// secret here would silently split the fleet in two.
func (s *SQLStore) LoadOrCreate(ctx context.Context) ([]byte, error) {
	secret, err := s.load(ctx)
	if err == nil {
		return secret, nil
	}

	if !errors.Is(err, errNoSecret) {
		return nil, err
	}

	candidate := s.seed
	if len(candidate) == 0 {
		if candidate, err = newSecret(); err != nil {
			return nil, err
		}
	}

	stmt := s.rebind(fmt.Sprintf(
		`INSERT INTO %s (id, secret, updated_at) VALUES (?, ?, ?) ON CONFLICT (id) DO NOTHING`, s.table))

	if _, execErr := s.db.ExecContext(ctx, stmt, secretRow, candidate, time.Now().Unix()); execErr != nil {
		return nil, fmt.Errorf("hubsecret: create: %w", execErr)
	}

	// Read back rather than returning the candidate: another hub may
	// have won the insert, and its value is the one that counts.
	return s.Load(ctx)
}

// Load returns the shared secret, erroring when none is stored.
func (s *SQLStore) Load(ctx context.Context) ([]byte, error) {
	secret, err := s.load(ctx)
	if errors.Is(err, errNoSecret) {
		return nil, errors.New("hubsecret: no secret in the shared database (run the hub first?)")
	}

	return secret, err
}

// Rotate replaces the shared secret, which invalidates every JWT the
// fleet has issued.
func (s *SQLStore) Rotate(ctx context.Context) ([]byte, error) {
	secret, err := newSecret()
	if err != nil {
		return nil, err
	}

	stmt := s.rebind(fmt.Sprintf(
		`INSERT INTO %s (id, secret, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT (id) DO UPDATE SET secret = excluded.secret, updated_at = excluded.updated_at`, s.table))

	if _, execErr := s.db.ExecContext(ctx, stmt, secretRow, secret, time.Now().Unix()); execErr != nil {
		return nil, fmt.Errorf("hubsecret: rotate: %w", execErr)
	}

	return secret, nil
}

// errNoSecret marks "the row is not there yet", which LoadOrCreate
// treats as work to do and Load treats as an error.
var errNoSecret = errors.New("hubsecret: no stored secret")

func (s *SQLStore) load(ctx context.Context) ([]byte, error) {
	query := s.rebind(fmt.Sprintf(`SELECT secret FROM %s WHERE id = ?`, s.table))

	var secret []byte

	err := s.db.QueryRowContext(ctx, query, secretRow).Scan(&secret)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNoSecret
	}

	if err != nil {
		return nil, fmt.Errorf("hubsecret: load: %w", err)
	}

	if len(secret) == 0 {
		return nil, errNoSecret
	}

	return secret, nil
}

// bytesColumn is the raw-bytes column type per engine.
func (s *SQLStore) bytesColumn() string {
	if s.dialect == Postgres {
		return "BYTEA"
	}

	return "BLOB"
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
