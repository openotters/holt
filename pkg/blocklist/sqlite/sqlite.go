// Package sqlite is the SQLite flavour of the SQL-backed denylist.
// The consumer brings the driver (modernc.org/sqlite,
// mattn/go-sqlite3, …) and hands in an opened *sql.DB.
package sqlite

import (
	"database/sql"

	"github.com/openotters/holt/pkg/blocklist"
)

// New returns a SQLite-dialect denylist store over db. Call Migrate
// once to create the table.
func New(db *sql.DB, opts ...blocklist.SQLOption) *blocklist.SQLStore {
	return blocklist.NewSQL(db, blocklist.SQLite, opts...)
}
