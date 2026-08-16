// Package sqlite is the SQLite flavour of the SQL-backed presence
// directory. The consumer brings the driver (modernc.org/sqlite,
// mattn/go-sqlite3, …) and hands in an opened *sql.DB.
package sqlite

import (
	"database/sql"

	"github.com/openotters/holt/internal/directory/sqldir"
)

// New returns a SQLite-dialect presence directory over db. Call
// Migrate once to create the table.
func New(db *sql.DB, opts ...sqldir.Option) *sqldir.Directory {
	return sqldir.New(db, sqldir.SQLite, opts...)
}
