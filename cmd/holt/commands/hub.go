package commands

import (
	"context"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL driver, registered as "pgx" (--directory-dsn)
	"go.opentelemetry.io/otel"
	"go.uber.org/zap"

	c "github.com/merlindorin/go-shared/pkg/cmd"

	"github.com/openotters/holt/cmd/holt/internal/httpsrv"
	"github.com/openotters/holt/cmd/holt/internal/hubmetrics"
	"github.com/openotters/holt/cmd/holt/internal/hubsecret"
	"github.com/openotters/holt/cmd/holt/internal/store"
	"github.com/openotters/holt/cmd/holt/internal/style"
)

// Hub runs the reverse-tunnel hub with three listeners: a JWT-auth
// WebSocket tunnel endpoint peers attach to, an Admin gRPC endpoint
// (list, stop, block), and a routed proxy that reaches peer services.
// All three are plaintext: transport encryption is the deployment's job
// (a TLS edge, ingress, or mesh in front of the hub).
type Hub struct {
	TunnelAddr string `help:"JWT-auth listener where peers attach over a WebSocket (plaintext; front with TLS)." default:"127.0.0.1:7200"`
	AdminAddr  string `help:"Admin gRPC listener (list/stop/block); also serves the console with --ui." default:"127.0.0.1:7201"`
	ProxyAddr  string `help:"Header-routed proxy to reach peer services (x-tunnel-peer)." default:"127.0.0.1:7202"`
	State      string `help:"Directory for the hub JWT secret + state (default: ~/.holt)." type:"path"`

	// Storage backend for tunnel presence AND the peer denylist: by
	// default both share the local SQLite state DB. A PostgreSQL DSN
	// moves them to a shared database instead, so a fleet of hubs sees
	// which peer is attached where and shares its blocks. The JWT
	// secret stays local either way.
	DirectoryDSN string        `help:"PostgreSQL DSN for a shared presence directory + blocklist (e.g. postgres://user:pass@host/db). Empty keeps both in the local SQLite state." name:"directory-dsn"`
	UI           bool          `help:"Serve the web console (and its enroll endpoint) on the admin listener."`
	UIPath       string        `help:"Serve the console from this directory instead of the embedded build." type:"path"`
	TokenTTL     time.Duration `help:"Lifetime of JWTs minted by enroll." default:"24h"`

	// Public tunnel URL stamped into join tokens (what peers dial). Its
	// scheme selects the peer transport: wss dials TLS under the
	// WebSocket (to a TLS edge in front of the hub), ws dials
	// plaintext; https/http are accepted as aliases. Defaults to
	// ws://<tunnel-addr>, but that is the BIND address; behind a
	// LoadBalancer, NAT, or TLS edge it differs, set the reachable URL.
	AdvertiseAddr string `help:"Public tunnel URL peers dial, stamped into tokens (e.g. wss://holt.example.com; default: ws://<tunnel-addr>)." name:"advertise-addr"`

	// The admin listener has no built-in auth (mint token, kill, block);
	// it is meant to sit behind an authenticating proxy or stay on
	// loopback. These are hardening knobs, not a substitute for that.
	AllowedHosts []string `help:"Extra Host values the admin/console accept, on top of loopback (defeats DNS rebinding when exposed). Set your public hostname here; use '*' to disable the check." name:"allowed-host"`
	MaxConns     int      `help:"Cap concurrent tunnel connections (0 = unlimited)." default:"0"`

	// Public base URL the proxy is reachable at (e.g. behind an ingress
	// or a zero trust tunnel). Shown in the console's "Call" command so
	// operators get the externally-reachable curl, not just localhost.
	ExternalURL string `help:"Public base URL the proxy is reachable at, shown in the console's Call command (e.g. https://peers.example.com)." name:"external-url"`

	// How the proxy picks the target peer. The header works anywhere
	// (no DNS needed); subdomain routing gives every peer its own
	// hostname, which is what ordinary clients (browsers, webhooks,
	// anything that only takes a URL) can actually use.
	ProxyRouting string `help:"How the proxy picks the target peer: header (x-tunnel-peer), subdomain (<peer>.<proxy-domain>), or both." enum:"header,subdomain,both" default:"header" name:"proxy-routing"`
	ProxyDomain  string `help:"Base domain for subdomain routing, e.g. peers.example.com (required with --proxy-routing subdomain|both)." name:"proxy-domain"`

	// Prometheus metrics: the hub already records OTel instruments
	// (holt.tunnels.active / .attaches / .detaches); this exposes them
	// on a /metrics endpoint via an OTel Prometheus exporter.
	Metrics     bool   `help:"Serve Prometheus metrics on /metrics."`
	MetricsAddr string `help:"Metrics listener address." default:"127.0.0.1:7203"`
}

// gracePeriod bounds how long shutdown waits for tunnels and listeners
// to drain before exiting anyway.
const gracePeriod = 5 * time.Second

// Run starts the hub and blocks until the context is cancelled.
func (h *Hub) Run(ctx context.Context, commons *c.Commons, logger *zap.Logger, out *style.Output) error {
	logger = logger.Named("hub")

	if h.State == "" {
		h.State = defaultStateDir()
	}

	// Subdomain routing without a domain to strip would match every
	// host and route to nonsense; a domain the strategy never reads is
	// just as misleading. Fail at boot, not per request.
	if _, err := h.routing().Resolvers(h.ProxyDomain); err != nil {
		return fmt.Errorf("%w (--proxy-routing / --proxy-domain)", err)
	}

	// The local SQLite DB is the default backend for tunnel presence,
	// the blocklist, and the signing secret (see openBackends).
	st, err := store.Open(h.State)
	if err != nil {
		return err
	}

	defer func() { _ = st.Close() }()

	// Debug, not info: the welcome banner (or the "hub up" JSON line)
	// already tells the operator where state lives.
	logger.Debug("hub state ready", zap.String("dir", h.State))

	// Tunnel presence, the peer denylist, and the hub's identity live
	// in the same SQL backend: the local SQLite DB by default, or a
	// shared PostgreSQL with --directory-dsn, so a fleet of hubs sees
	// each other's peers and blocks, and signs with one key.
	back, err := h.openBackends(ctx, st)
	if err != nil {
		return err
	}

	defer back.close()

	dir, blockStore := back.directory, back.blocks

	if migErr := dir.Migrate(ctx); migErr != nil {
		return migErr
	}

	if migErr := blockStore.Migrate(ctx); migErr != nil {
		return migErr
	}

	if sqlSecret, ok := back.secret.(*hubsecret.SQLStore); ok {
		if migErr := sqlSecret.Migrate(ctx); migErr != nil {
			return migErr
		}
	}

	// The signing secret is held behind an atomic so rotate-secret can
	// swap it live (invalidating every issued JWT) without a restart,
	// here or on another hub sharing the backend.
	secret, err := back.secret.LoadOrCreate(ctx)
	if err != nil {
		return err
	}

	logger.Debug("hub identity ready", zap.String("stored in", back.secret.Describe()))

	// Prometheus metrics: install the OTel SDK provider globally before
	// any instrument is built, so they all bind to it. When off,
	// everything records against the global no-op provider.
	if h.Metrics {
		mp, mpErr := hubmetrics.Provider()
		if mpErr != nil {
			return mpErr
		}

		otel.SetMeterProvider(mp)

		defer func() { _ = mp.Shutdown(context.Background()) }()
	}

	rt, err := h.newRuntime(ctx, commons, logger, dir, blockStore, back.secret, secret)
	if err != nil {
		return err
	}

	// On a shared backend, a rotation performed on another hub has to
	// reach this one; on a local one this is a no-op.
	watchIdentity(ctx, back.secret, rt.secrets, rt.registry, logger)

	listeners := httpsrv.NewGroup(logger)
	if err = h.startServers(ctx, listeners, rt); err != nil {
		return err
	}

	// The banner replaces the "hub up" log line for humans; production
	// (--log-format json) keeps the structured line instead.
	if out.Pretty {
		fmt.Print(h.welcomeBanner())
	} else {
		logger.Info("hub up", h.logFields()...)
	}

	<-ctx.Done()

	// The terminal echoes "^C" with no newline; emit one so the
	// shutdown logs start on their own line. Pretty (interactive) only,
	// so JSON log streams stay clean.
	if out.Pretty {
		fmt.Println()
	}

	h.shutdown(listeners, rt, logger, out.Pretty)

	return nil
}

// shutdown drains the listeners after Ctrl-C. Everything past the
// welcome banner is a plain log line (the hub is a server now), so this
// logs rather than prints. A second signal during the grace period
// force-closes immediately instead of waiting the drain out.
func (h *Hub) shutdown(listeners *httpsrv.Group, rt *hubRuntime, logger *zap.Logger, pretty bool) {
	logger.Info("shutting down, draining listeners (ctrl-c again to force)",
		zap.Int("tunnels", rt.registry.CountTunnels()), zap.Duration("grace", gracePeriod))

	rt.registry.StopAllTunnels("shutting-down")

	if !listeners.Drain(gracePeriod) {
		logger.Info("stopped cleanly")

		return
	}

	// Clear the second "^C" the terminal just echoed.
	if pretty {
		fmt.Println()
	}

	logger.Warn("forced shutdown on second signal")
}
