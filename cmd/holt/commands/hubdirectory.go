package commands

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"

	"github.com/openotters/holt/cmd/holt/internal/hubsecret"
	"github.com/openotters/holt/cmd/holt/internal/store"
	"github.com/openotters/holt/pkg/blocklist"
	blockpg "github.com/openotters/holt/pkg/blocklist/postgres"
	blocklite "github.com/openotters/holt/pkg/blocklist/sqlite"
	"github.com/openotters/holt/pkg/directory/postgres"
	"github.com/openotters/holt/pkg/directory/sqldir"
	"github.com/openotters/holt/pkg/directory/sqlite"
)

// backends is everything the hub persists, on whichever engine it was
// pointed at: tunnel presence, the peer denylist, and the JWT signing
// secret that is the hub's identity.
type backends struct {
	directory *sqldir.Directory
	blocks    *blocklist.SQLStore
	secret    hubsecret.Store
	close     func()
}

// openBackends picks the storage backend: the local SQLite state DB by
// default, or a shared PostgreSQL when --directory-dsn is set — so a
// fleet of hubs sees each other's peers, each other's blocks, and
// signs with one identity. The returned close func releases the
// PostgreSQL pool (a no-op for SQLite, whose DB belongs to the store).
func (h *Hub) openBackends(ctx context.Context, st *store.Store) (backends, error) {
	if h.DirectoryDSN == "" {
		return backends{
			directory: sqlite.New(st.DB()),
			blocks:    blocklite.New(st.DB()),
			secret:    hubsecret.NewFile(h.State),
			close:     func() {},
		}, nil
	}

	db, err := sql.Open("pgx", h.DirectoryDSN)
	if err != nil {
		return backends{}, fmt.Errorf("directory: open postgres: %w", err)
	}

	// Fail at boot with a clear error, not on the first attach.
	if pingErr := db.PingContext(ctx); pingErr != nil {
		_ = db.Close()

		return backends{}, fmt.Errorf("directory: ping postgres: %w", pingErr)
	}

	// A hub that already has a file secret adopts it into the shared
	// database rather than minting a new one, so moving identity off
	// the volume does not invalidate the tokens already in the field.
	secret := hubsecret.NewSQL(db, hubsecret.Postgres, hubsecret.WithSeed(hubsecret.PeekFile(h.State)))

	return backends{
		directory: postgres.New(db),
		blocks:    blockpg.New(db),
		secret:    secret,
		close:     func() { _ = db.Close() },
	}, nil
}

// openSecretStore resolves where the signing secret lives for the
// commands that touch it outside the hub process (enroll's local mode,
// rotate-secret): the shared database when a DSN is configured,
// otherwise the state directory.
func openSecretStore(ctx context.Context, state, dsn string) (hubsecret.Store, func(), error) {
	if dsn == "" {
		return hubsecret.NewFile(state), func() {}, nil
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("identity: open postgres: %w", err)
	}

	if pingErr := db.PingContext(ctx); pingErr != nil {
		_ = db.Close()

		return nil, nil, fmt.Errorf("identity: ping postgres: %w", pingErr)
	}

	return hubsecret.NewSQL(db, hubsecret.Postgres), func() { _ = db.Close() }, nil
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
