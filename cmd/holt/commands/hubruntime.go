package commands

import (
	"context"
	"os"

	"go.uber.org/zap"

	c "github.com/merlindorin/go-shared/pkg/cmd"

	"github.com/openotters/holt/cmd/holt/internal/blocklist"
	"github.com/openotters/holt/cmd/holt/internal/hubmetrics"
	"github.com/openotters/holt/cmd/holt/internal/jwtauth"
	"github.com/openotters/holt/cmd/holt/internal/store"
	"github.com/openotters/holt/internal/admin"
	"github.com/openotters/holt/internal/directory/sqldir"
	"github.com/openotters/holt/internal/registry"
)

// hubRuntime is what the four listeners share: the registry of live
// tunnels, the credentials they are gated on, the instruments they
// record to, and the static metadata they report. Run owns the
// resources these are built from (the state DB, the directory, the
// meter provider) and closes them; nothing here needs closing.
type hubRuntime struct {
	registry *registry.Registry
	blocks   *blocklist.List
	secrets  *jwtauth.Secret
	metrics  *hubmetrics.Metrics
	info     admin.HubInfo
	logger   *zap.Logger
}

// newRuntime wires the shared pieces together over an open store and
// presence directory. Stale presence rows from a previous run are
// cleared here, before any listener can accept an attach.
func (h *Hub) newRuntime(
	ctx context.Context, commons *c.Commons, logger *zap.Logger,
	st *store.Store, dir *sqldir.Directory, secret []byte,
) (*hubRuntime, error) {
	registry := registry.NewRegistry(logger, registry.WithHubID(hostname()), registry.WithDirectory(dir))
	if err := registry.ClearStale(ctx); err != nil {
		return nil, err
	}

	blocks, err := blocklist.New(st)
	if err != nil {
		return nil, err
	}

	return &hubRuntime{
		registry: registry,
		blocks:   blocks,
		secrets:  jwtauth.NewSecret(secret),
		metrics:  hubmetrics.New(commons.Version.Version(), commons.Version.Commit()),
		info:     h.adminInfo(commons),
		logger:   logger,
	}, nil
}

// hostname identifies this hub in the shared presence directory, so a
// fleet's rows say which hub a peer is attached to.
func hostname() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}

	return "hub"
}
