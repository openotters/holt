package commands

import (
	"context"
	"os"

	"go.uber.org/zap"

	c "github.com/merlindorin/go-shared/pkg/cmd"

	"github.com/openotters/holt/cmd/holt/internal/admin"
	"github.com/openotters/holt/cmd/holt/internal/hubmetrics"
	"github.com/openotters/holt/cmd/holt/internal/hubsecret"
	"github.com/openotters/holt/pkg/blocklist"
	"github.com/openotters/holt/pkg/directory/sqldir"
	"github.com/openotters/holt/pkg/jwtauth"
	"github.com/openotters/holt/pkg/registry"
	"github.com/openotters/holt/pkg/reqlog"
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
	identity hubsecret.Store
	metrics  *hubmetrics.Metrics
	info     admin.HubInfo
	logger   *zap.Logger
	// requests fans what the proxy carried out to console watchers; a
	// few recent events in memory, never logged or stored.
	requests *reqlog.Broker
}

// newRuntime wires the shared pieces together over the opened
// presence directory and denylist store. Stale presence rows from a
// previous run are cleared here, before any listener can accept an
// attach.
func (h *Hub) newRuntime(
	ctx context.Context, commons *c.Commons, logger *zap.Logger,
	dir *sqldir.Directory, blockStore blocklist.Store, identity hubsecret.Store, secret []byte,
) (*hubRuntime, error) {
	registry := registry.NewRegistry(logger, registry.WithHubID(hostname()), registry.WithDirectory(dir))
	if err := registry.ClearStale(ctx); err != nil {
		return nil, err
	}

	blocks, err := blocklist.New(ctx, blockStore)
	if err != nil {
		return nil, err
	}

	return &hubRuntime{
		registry: registry,
		blocks:   blocks,
		secrets:  jwtauth.NewSecret(secret),
		identity: identity,
		metrics:  hubmetrics.New(commons.Version.Version(), commons.Version.Commit()),
		info:     h.adminInfo(commons),
		logger:   logger,
		requests: reqlog.NewBroker(trafficWindow(h.TrafficBuffer)),
	}, nil
}

// trafficWindow maps the flag onto the broker's convention, where 0
// means "the default" and a negative number means "keep none": an
// operator who asks for 0 wants none, not the default.
func trafficWindow(events int) int {
	if events <= 0 {
		return -1
	}

	return events
}

// hostname identifies this hub in the shared presence directory, so a
// fleet's rows say which hub a peer is attached to.
func hostname() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}

	return "hub"
}
