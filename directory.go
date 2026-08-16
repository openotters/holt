package holt

import (
	"database/sql"

	"github.com/openotters/holt/internal/directory/postgres"
	"github.com/openotters/holt/internal/directory/sqldir"
	"github.com/openotters/holt/internal/directory/sqlite"
)

// SQLDirectory is a SQL-backed Directory: a shared peer-presence
// store so a fleet of hubs can each see who is attached where. Build
// one with NewSQLiteDirectory or NewPostgresDirectory and call
// Migrate once; it imports only database/sql, so the consumer brings
// the driver and hands in an opened *sql.DB.
type SQLDirectory = sqldir.Directory

// SQLDirectoryOption configures a SQLDirectory.
type SQLDirectoryOption = sqldir.Option

// WithSQLTable overrides the presence table name (default
// "holt_peers").
func WithSQLTable(name string) SQLDirectoryOption { return sqldir.WithTable(name) }

// NewSQLiteDirectory returns a SQLite-dialect presence directory over
// db (driver: modernc.org/sqlite, mattn/go-sqlite3, …).
func NewSQLiteDirectory(db *sql.DB, opts ...SQLDirectoryOption) *SQLDirectory {
	return sqlite.New(db, opts...)
}

// NewPostgresDirectory returns a Postgres-dialect presence directory
// over db (driver: jackc/pgx/stdlib, lib/pq, …).
func NewPostgresDirectory(db *sql.DB, opts ...SQLDirectoryOption) *SQLDirectory {
	return postgres.New(db, opts...)
}
