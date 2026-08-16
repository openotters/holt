// Package postgres is the PostgreSQL flavour of the SQL-backed
// presence directory. The consumer brings the driver
// (jackc/pgx/stdlib, lib/pq, …) and hands in an opened *sql.DB.
package postgres

import (
	"database/sql"

	"github.com/openotters/holt/internal/directory/sqldir"
)

// New returns a Postgres-dialect presence directory over db. Call
// Migrate once to create the table.
func New(db *sql.DB, opts ...sqldir.Option) *sqldir.Directory {
	return sqldir.New(db, sqldir.Postgres, opts...)
}
