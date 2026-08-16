// Package postgres is the PostgreSQL flavour of the SQL-backed
// denylist. The consumer brings the driver (jackc/pgx/stdlib,
// lib/pq, …) and hands in an opened *sql.DB.
package postgres

import (
	"database/sql"

	"github.com/openotters/holt/pkg/blocklist"
)

// New returns a Postgres-dialect denylist store over db. Call Migrate
// once to create the table.
func New(db *sql.DB, opts ...blocklist.SQLOption) *blocklist.SQLStore {
	return blocklist.NewSQL(db, blocklist.Postgres, opts...)
}
