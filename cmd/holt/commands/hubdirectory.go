package commands

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"

	"github.com/openotters/holt/cmd/holt/internal/store"
	"github.com/openotters/holt/internal/directory/postgres"
	"github.com/openotters/holt/internal/directory/sqldir"
	"github.com/openotters/holt/internal/directory/sqlite"
)

// openDirectory picks the presence-directory backend: the local SQLite
// state DB by default, or a shared PostgreSQL when --directory-dsn is
// set. The returned close func releases the PostgreSQL pool (a no-op
// for SQLite, whose DB belongs to the store).
func (h *Hub) openDirectory(ctx context.Context, st *store.Store) (*sqldir.Directory, func(), error) {
	if h.DirectoryDSN == "" {
		return sqlite.New(st.DB()), func() {}, nil
	}

	db, err := sql.Open("pgx", h.DirectoryDSN)
	if err != nil {
		return nil, nil, fmt.Errorf("directory: open postgres: %w", err)
	}

	// Fail at boot with a clear error, not on the first attach.
	if pingErr := db.PingContext(ctx); pingErr != nil {
		_ = db.Close()

		return nil, nil, fmt.Errorf("directory: ping postgres: %w", pingErr)
	}

	return postgres.New(db), func() { _ = db.Close() }, nil
}

// redactDSN is the display form of the directory DSN: URL-form DSNs
// get their password masked; anything else (key=value form) is hidden
// entirely rather than risk echoing a credential.
func redactDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil || u.Host == "" {
		return "postgres (DSN hidden)"
	}

	return u.Redacted()
}
