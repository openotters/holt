package commands

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL driver, registered as "pgx" (--directory-dsn)
	c "github.com/merlindorin/go-shared/pkg/cmd"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	promexporter "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.uber.org/zap"
	"golang.org/x/net/netutil"

	"github.com/openotters/holt"
	holtv1connect "github.com/openotters/holt/api/v1/holtv1connect"
	"github.com/openotters/holt/cmd/holt/internal/hubsecret"
	"github.com/openotters/holt/cmd/holt/internal/jwtauth"
	"github.com/openotters/holt/cmd/holt/internal/store"
	"github.com/openotters/holt/cmd/holt/internal/style"
	"github.com/openotters/holt/cmd/holt/internal/token"
	"github.com/openotters/holt/cmd/holt/internal/webui"
	"github.com/openotters/holt/hub"
	"github.com/openotters/holt/hub/admin"
	"github.com/openotters/holt/hub/sqldir"
)

const routeHeader = "x-tunnel-peer"

type peerCtxKey struct{}

// secretState holds the hub's JWT signing secret behind an atomic
// pointer, so `holt rotate-secret` from the console can swap it live —
// invalidating every issued JWT — without a process restart.
type secretState struct {
	v atomic.Pointer[[]byte]
}

func (s *secretState) get() []byte {
	if p := s.v.Load(); p != nil {
		return *p
	}

	return nil
}

func (s *secretState) set(b []byte) { s.v.Store(&b) }

// Hub runs the reverse-tunnel hub with three listeners: a JWT-auth
// WebSocket tunnel endpoint peers attach to, an Admin gRPC endpoint
// (list, stop, block), and a header-routed proxy that reaches peer
// services. All three are plaintext: transport encryption is the
// deployment's job (a TLS edge, ingress, or mesh in front of the hub).
type Hub struct {
	TunnelAddr string `help:"JWT-auth listener where peers attach over a WebSocket (plaintext; front with TLS)." default:"127.0.0.1:7000"`
	AdminAddr  string `help:"Admin gRPC listener (list/stop/block); also serves the console with --ui." default:"127.0.0.1:7001"`
	ProxyAddr  string `help:"Header-routed proxy to reach peer services (x-tunnel-peer)." default:"127.0.0.1:7002"`
	State      string `help:"Directory for the hub JWT secret + state (default: ~/.holt)." type:"path"`

	// Presence backend: by default the tunnel-presence directory shares
	// the local SQLite state DB. A PostgreSQL DSN moves it to a shared
	// database instead, so a fleet of hubs can see which peer is
	// attached where. The JWT secret and blocklist stay local either
	// way — this is presence only.
	DirectoryDSN string        `help:"PostgreSQL DSN for a shared presence directory (e.g. postgres://user:pass@host/db). Empty keeps presence in the local SQLite state." name:"directory-dsn"`
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

	// Prometheus metrics: the hub already records OTel instruments
	// (holt.tunnels.active / .attaches / .detaches); this exposes them
	// on a /metrics endpoint via an OTel Prometheus exporter.
	Metrics     bool   `help:"Serve Prometheus metrics on /metrics."`
	MetricsAddr string `help:"Metrics listener address." default:"127.0.0.1:7003"`
}

// Run starts the hub and blocks until the context is cancelled.
func (h *Hub) Run(ctx context.Context, commons *c.Commons, logger *zap.Logger, out *style.Output) error {
	logger = logger.Named("hub")

	if h.State == "" {
		h.State = defaultStateDir()
	}

	// The JWT signing secret persists as a file in the state folder,
	// held behind an atomic so the console's rotate-secret can swap it
	// live (invalidating every issued JWT) without a restart.
	secret, err := hubsecret.LoadOrCreate(h.State)
	if err != nil {
		return err
	}

	secrets := &secretState{}
	secrets.set(secret)

	// Everything else (blocklist, tunnel presence) lives in a SQLite DB
	// alongside it.
	st, err := store.Open(h.State)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	// Debug, not info: the welcome banner (or the "hub up" JSON line)
	// already tells the operator where state lives.
	logger.Debug("hub state ready", zap.String("dir", h.State))

	// Tunnel presence is projected into a SQL Directory: the same
	// SQLite DB by default, or a shared PostgreSQL with --directory-dsn
	// (so a fleet of hubs sees each other's peers); stale rows from a
	// previous run are cleared on boot.
	dir, closeDir, err := h.openDirectory(ctx, st)
	if err != nil {
		return err
	}
	defer closeDir()

	if migErr := dir.Migrate(ctx); migErr != nil {
		return migErr
	}

	// Prometheus metrics: install the OTel SDK provider globally (before
	// building the registry and the CLI instruments, so both bind to
	// it), and promhttp serves it below. When off, everything records
	// against the global no-op provider.
	if h.Metrics {
		mp, mpErr := meterProvider()
		if mpErr != nil {
			return mpErr
		}

		otel.SetMeterProvider(mp)

		defer func() { _ = mp.Shutdown(context.Background()) }()
	}

	registry := hub.NewRegistry(logger, hub.WithHubID(hostname()), hub.WithDirectory(dir))
	if clearErr := registry.ClearStale(ctx); clearErr != nil {
		return clearErr
	}

	blocks, err := newBlockList(st)
	if err != nil {
		return err
	}

	metrics := newHubMetrics(commons.Version.Version(), commons.Version.Commit())

	info := h.adminInfo(commons)

	servers, err := h.startServers(registry, blocks, secrets, metrics, info, logger)
	if err != nil {
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

	h.shutdown(registry, servers, logger, out.Pretty)

	return nil
}

// openDirectory picks the presence-directory backend: the local SQLite
// state DB by default, or a shared PostgreSQL when --directory-dsn is
// set. The returned close func releases the PostgreSQL pool (a no-op
// for SQLite, whose DB belongs to the store).
func (h *Hub) openDirectory(ctx context.Context, st *store.Store) (*sqldir.Directory, func(), error) {
	if h.DirectoryDSN == "" {
		return sqldir.New(st.DB(), sqldir.SQLite), func() {}, nil
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

	return sqldir.New(db, sqldir.Postgres), func() { _ = db.Close() }, nil
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

// shutdown drains the listeners after Ctrl-C. Everything past the
// welcome banner is a plain log line (the hub is a server now), so this
// logs rather than prints. A second signal during the grace period
// force-closes immediately instead of waiting the drain out.
func (h *Hub) shutdown(registry *hub.Registry, servers []*http.Server, logger *zap.Logger, pretty bool) {
	logger.Info("shutting down, draining listeners (ctrl-c again to force)",
		zap.Int("tunnels", registry.CountTunnels()), zap.Duration("grace", gracePeriod))

	registry.StopAllTunnels("shutting-down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), gracePeriod)
	defer cancel()

	// The parent context already consumed the first signal, so re-arm
	// to catch a second Ctrl-C during the grace period.
	hardStop := make(chan os.Signal, 1)
	signal.Notify(hardStop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(hardStop)

	drained := make(chan struct{})
	go func() {
		for _, srv := range servers {
			_ = srv.Shutdown(shutdownCtx)
		}

		close(drained)
	}()

	forceClose := func() {
		cancel()

		for _, srv := range servers {
			_ = srv.Close()
		}
	}

	if awaitShutdown(drained, hardStop, forceClose) {
		// Clear the second "^C" the terminal just echoed.
		if pretty {
			fmt.Println()
		}

		logger.Warn("forced shutdown on second signal")
	} else {
		logger.Info("stopped cleanly")
	}
}

// awaitShutdown blocks until the graceful drain finishes or a second
// signal arrives. On the signal it force-closes and reports true, so a
// second Ctrl-C ends the process now instead of waiting out the grace
// period.
func awaitShutdown(drained <-chan struct{}, hardStop <-chan os.Signal, forceClose func()) bool {
	select {
	case <-drained:
		return false
	case <-hardStop:
		forceClose()

		return true
	}
}

// gracePeriod bounds how long shutdown waits for tunnels and listeners
// to drain before exiting anyway.
const gracePeriod = 5 * time.Second

// startServers boots the tunnel, admin, proxy, and (optional) metrics
// listeners and returns them for shutdown.
func (h *Hub) startServers(
	registry *hub.Registry, blocks *blockList, secrets *secretState,
	metrics *hubMetrics, info admin.HubInfo, logger *zap.Logger,
) ([]*http.Server, error) {
	tunnelSrv, err := h.serveTunnels(registry, secrets, blocks, metrics, logger)
	if err != nil {
		return nil, err
	}

	adminSrv, err := h.serveAdmin(registry, blocks, secrets, info, logger)
	if err != nil {
		return nil, err
	}

	proxySrv, err := h.serveProxy(registry, metrics)
	if err != nil {
		return nil, err
	}

	servers := []*http.Server{tunnelSrv, adminSrv, proxySrv}

	if h.Metrics {
		metricsSrv, metricsErr := h.serveMetrics(logger)
		if metricsErr != nil {
			return nil, metricsErr
		}

		servers = append(servers, metricsSrv)
	}

	return servers, nil
}

// logFields is the "hub up" structured line for --log-format json.
func (h *Hub) logFields() []zap.Field {
	fields := []zap.Field{
		zap.String("tunnel", h.TunnelAddr),
		zap.String("admin", h.AdminAddr),
		zap.String("proxy", h.ProxyAddr),
	}
	if h.DirectoryDSN != "" {
		fields = append(fields, zap.String("directory", redactDSN(h.DirectoryDSN)))
	}
	if h.UI {
		fields = append(fields, zap.String("console", "http://"+h.AdminAddr+"/"))
	}

	if h.Metrics {
		fields = append(fields, zap.String("metrics", "http://"+h.MetricsAddr+"/metrics"))
	}

	return fields
}

// welcomeBanner renders the pretty startup block with the addresses
// and a first-step hint.
func (h *Hub) welcomeBanner() string {
	rows := []style.BannerRow{
		{Key: "tunnel", Value: h.TunnelAddr, Hint: "peers attach here (JWT auth; put TLS in front)"},
	}
	if h.AdvertiseAddr != "" {
		rows = append(rows,
			style.BannerRow{Key: "advertise", Value: h.advertiseURL(), Hint: "URL stamped into tokens"})
	}

	rows = append(rows, style.BannerRow{Key: "admin", Value: h.AdminAddr, Hint: "holt ls / kill / block"})
	if h.UI {
		rows = append(rows,
			style.BannerRow{Key: "console", Value: "http://" + h.AdminAddr + "/", Hint: "web console"})
	}

	rows = append(rows,
		style.BannerRow{Key: "proxy", Value: h.ProxyAddr, Hint: "reach peers: curl -H 'x-tunnel-peer: <peer>'"})

	if h.Metrics {
		rows = append(rows,
			style.BannerRow{Key: "metrics", Value: h.MetricsAddr + "/metrics", Hint: "prometheus metrics"})
	}

	if h.ExternalURL != "" {
		rows = append(rows, style.BannerRow{
			Key:   "external",
			Value: strings.TrimRight(h.ExternalURL, "/"),
			Hint:  "public URL peers are reached at",
		})
	}

	rows = append(rows,
		style.BannerRow{Key: "state", Value: tildePath(h.State), Hint: "JWT secret, blocklist"})

	if h.DirectoryDSN != "" {
		rows = append(rows, style.BannerRow{
			Key:   "directory",
			Value: redactDSN(h.DirectoryDSN),
			Hint:  "shared presence (postgres)",
		})
	}

	return style.Banner("holt is up", rows, "enroll your first peer:  holt enroll <name>")
}

// serveTunnels runs the plaintext tunnel listener; peers upgrade to a
// WebSocket that carries the tunnel frames, authenticating with a JWT
// whose subject becomes the tunnel key. A blocked subject is rejected
// even with a valid token. Transport encryption is expected from the
// network in front of the hub (a TLS edge, ingress, or mesh), same as
// the proxy and admin listeners.
func (h *Hub) serveTunnels(
	registry *hub.Registry, secrets *secretState, blocks *blockList, metrics *hubMetrics, logger *zap.Logger,
) (*http.Server, error) {
	identity := func(ctx context.Context) (string, error) {
		peer, _ := ctx.Value(peerCtxKey{}).(string)
		if peer == "" {
			return "", errors.New("unauthenticated")
		}

		return peer, nil
	}

	// The upgrade is accepted on ANY path, so an advertise URL keeps
	// working whether the ingress in front routes /, a dedicated
	// prefix, or the pre-0.13 gRPC path. The secret is read
	// per-request from the atomic holder, so a rotate-secret takes
	// effect on the next attach without a restart.
	mux := http.NewServeMux()
	mux.Handle("/", jwtMiddleware(secrets, blocks, metrics, hub.NewHandler(registry, identity, logger)))

	srv := newH2CServer(mux)

	lis, err := listen(h.TunnelAddr)
	if err != nil {
		return nil, err
	}

	// Optional cap on concurrent tunnel connections. Off by default so
	// behavior is unchanged; a value bounds resource use under a flood
	// of attaches (each tunnel holds an HTTP/2 session).
	if h.MaxConns > 0 {
		lis = netutil.LimitListener(lis, h.MaxConns)
	}

	go func() {
		if serveErr := srv.Serve(lis); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			logger.Error("tunnel serve", zap.Error(serveErr))
		}
	}()

	return srv, nil
}

// serveAdmin runs the Admin gRPC service (list / stop / block) and,
// with --ui, the web console plus its enroll endpoint.
func (h *Hub) serveAdmin(
	registry *hub.Registry, blocks *blockList, secrets *secretState, info admin.HubInfo, logger *zap.Logger,
) (*http.Server, error) {
	mux := http.NewServeMux()

	adminPath, adminHandler := holtv1connect.NewAdminHandler(
		admin.NewService(registry, admin.WithBlocker(blocks), admin.WithInfo(info)),
	)
	mux.Handle(adminPath, adminHandler)

	// Liveness/readiness endpoint on the plaintext admin listener, so
	// probes never poke the TLS tunnel port (which would log an aborted
	// handshake). Exempt from the host guard below.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Enroll is always on so `holt enroll` works against a remote hub;
	// the console adds the rest of its endpoints only with --ui.
	h.mountEnroll(mux, secrets)

	if h.UI {
		h.mountConsole(mux, registry, secrets, logger)
	}

	// Host guard defeats DNS-rebinding against the plaintext console:
	// only loopback, the admin bind host, and any operator-configured
	// hostnames are accepted. Secure by default, opt out with '*'.
	adminHost := h.AdminAddr
	if hh, _, splitErr := net.SplitHostPort(h.AdminAddr); splitErr == nil {
		adminHost = hh
	}

	srv := newH2CServer(hostGuard(append([]string{adminHost}, h.AllowedHosts...), mux))

	lis, err := listen(h.AdminAddr)
	if err != nil {
		return nil, err
	}

	go func() {
		if serveErr := srv.Serve(lis); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			logger.Error("admin serve", zap.Error(serveErr))
		}
	}()

	return srv, nil
}

// hostGuard rejects requests whose Host header is not allow-listed,
// which is what stops a DNS-rebinding page from driving the plaintext,
// loopback-served console and its token-minting endpoint. Loopback
// names are always allowed; a deployment exposed through a proxy adds
// its public hostname. A single "*" entry disables the check.
func hostGuard(allowed []string, next http.Handler) http.Handler {
	allow := map[string]bool{"127.0.0.1": true, "localhost": true, "::1": true}
	wildcard := false

	for _, h := range allowed {
		if h == "*" {
			wildcard = true
		}

		if h != "" {
			allow[strings.ToLower(h)] = true
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Health checks carry the pod IP as Host and return no data, so
		// they are exempt from the allow-list.
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)

			return
		}

		if !wildcard {
			host := r.Host
			if hh, _, err := net.SplitHostPort(host); err == nil {
				host = hh
			}

			if !allow[strings.ToLower(host)] {
				http.Error(w, "forbidden: host not allowed (set --allowed-host)", http.StatusForbidden)

				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// adminInfo builds the static hub metadata the Admin Info RPC reports.
func (h *Hub) adminInfo(commons *c.Commons) admin.HubInfo {
	metricsAddr := ""
	if h.Metrics {
		metricsAddr = h.MetricsAddr
	}

	return admin.HubInfo{
		Version:       commons.Version.Version(),
		Commit:        commons.Version.Commit(),
		AdvertiseAddr: h.advertiseURL(),
		ProxyAddr:     h.ProxyAddr,
		RouteHeader:   routeHeader,
		MetricsAddr:   metricsAddr,
		ExternalURL:   strings.TrimRight(h.ExternalURL, "/"),
		TokenTTL:      h.TokenTTL,
	}
}

// advertiseURL is the tunnel URL stamped into tokens (what peers dial):
// the operator override if set, otherwise ws://<bind address>. A value
// without a scheme is assumed ws (plaintext), so peers get a
// well-formed URL and the scheme drives their transport.
func (h *Hub) advertiseURL() string {
	adv := h.AdvertiseAddr
	if adv == "" {
		adv = h.TunnelAddr
	}

	if !strings.Contains(adv, "://") {
		adv = "ws://" + adv
	}

	return adv
}

// mountEnroll registers POST /api/enroll on the admin listener. It is
// always on (not gated on --ui), so `holt enroll` can mint tokens
// against a remote hub — the hub supplies its own advertise address.
func (h *Hub) mountEnroll(mux *http.ServeMux, secrets *secretState) {
	mux.HandleFunc("POST /api/enroll", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Peer      string `json:"peer"`
			TunnelURL string `json:"tunnel_url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Peer == "" {
			http.Error(w, "peer is required", http.StatusBadRequest)

			return
		}

		// The tunnel URL stamped into the token: the caller's override if
		// given, otherwise the hub's advertised one. Signs with the
		// current secret so a rotate takes effect immediately.
		tunnelURL := body.TunnelURL
		if tunnelURL == "" {
			tunnelURL = h.advertiseURL()
		}

		tok, err := mintToken(secrets.get(), tunnelURL, body.Peer, h.TokenTTL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)

			return
		}

		writeJSON(w, map[string]string{
			"token":   tok,
			"command": "holt expose localhost:PORT --token " + tok,
		})
	})
}

// mountConsole registers the web console, its config/rotate endpoints,
// and the static build.
func (h *Hub) mountConsole(mux *http.ServeMux, registry *hub.Registry, secrets *secretState, logger *zap.Logger) {
	// Danger zone: rotate the JWT signing secret. Regenerates it on disk
	// and hot-swaps the live secret, so it takes effect immediately.
	// Every JWT already issued was signed with the old secret and stops
	// verifying, and live tunnels are closed; peers must be re-enrolled.
	mux.HandleFunc("POST /api/rotate-secret", func(w http.ResponseWriter, _ *http.Request) {
		secret, err := hubsecret.Rotate(h.State)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)

			return
		}

		secrets.set(secret)

		closed := registry.CountTunnels()
		registry.StopAllTunnels(holt.ReasonTokenRevoked)

		logger.Warn("hub signing secret rotated via console; tokens invalidated, tunnels closed",
			zap.Int("closed_tunnels", closed))

		writeJSON(w, map[string]any{"rotated": true, "closedTunnels": closed})
	})

	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]string{
			"routeHeader": routeHeader,
			"proxyPort":   portOf(h.ProxyAddr),
			"externalURL": strings.TrimRight(h.ExternalURL, "/"),
			"metricsPort": h.metricsPortForConfig(),
		})
	})

	mux.Handle("/", webui.Handler(h.UIPath))

	logger.Debug("web console enabled", zap.String("url", "http://"+h.AdminAddr+"/"))
}

// metricsPortForConfig is the metrics port advertised to the console,
// or empty when metrics are off.
func (h *Hub) metricsPortForConfig() string {
	if !h.Metrics {
		return ""
	}

	return portOf(h.MetricsAddr)
}

// portOf returns the port of a host:port address, or the whole string
// if it has no port.
func portOf(addr string) string {
	if _, p, err := net.SplitHostPort(addr); err == nil {
		return p
	}

	return addr
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// meterProvider builds an OTel SDK meter provider backed by a
// Prometheus exporter (registered with the default Prometheus registry
// that promhttp serves).
func meterProvider() (*sdkmetric.MeterProvider, error) {
	exporter, err := promexporter.New()
	if err != nil {
		return nil, fmt.Errorf("metrics: %w", err)
	}

	return sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter)), nil
}

// serveMetrics serves the OTel Prometheus exporter on /metrics.
func (h *Hub) serveMetrics(logger *zap.Logger) (*http.Server, error) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: readHeaderTimeout, IdleTimeout: idleTimeout}

	lis, err := listen(h.MetricsAddr)
	if err != nil {
		return nil, err
	}

	go func() {
		if serveErr := srv.Serve(lis); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			logger.Error("metrics serve", zap.Error(serveErr))
		}
	}()

	return srv, nil
}

// serveProxy runs the header-routed reverse proxy.
func (h *Hub) serveProxy(registry *hub.Registry, metrics *hubMetrics) (*http.Server, error) {
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = "peer.invalid"
			pr.Out.Host = "peer.invalid"
		},
		Transport:     peerRouter{registry: registry, metrics: metrics},
		FlushInterval: -1,
		ErrorHandler:  proxyError,
	}

	// A bare visit (no target peer named) gets a landing page, not a
	// proxied request, so it never turns into a 502.
	routed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(routeHeader) == "" {
			metrics.recordProxyError(r.Context(), "no-header")
			proxyLanding(w, r)

			return
		}

		proxy.ServeHTTP(w, r)
	})

	srv := newH2CServer(metrics.instrument(routed))

	lis, err := listen(h.ProxyAddr)
	if err != nil {
		return nil, err
	}

	go func() { _ = srv.Serve(lis) }()

	return srv, nil
}

// jwtMiddleware verifies the Bearer JWT, rejects blocked subjects, and
// stamps the peer id onto the request context for the identity func.
func jwtMiddleware(secrets *secretState, blocks *blockList, metrics *hubMetrics, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")

		peer, err := jwtauth.Verify(secrets.get(), bearer)
		if err != nil {
			metrics.recordReject(r.Context(), "unauthorized")
			http.Error(w, "unauthorized: "+err.Error(), http.StatusUnauthorized)

			return
		}

		if blocks.IsBlocked(peer) {
			metrics.recordReject(r.Context(), "blocked")
			http.Error(w, "forbidden: peer is blocked", http.StatusForbidden)

			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), peerCtxKey{}, peer)))
	})
}

// peerRouter dispatches each request down the tunnel named in the
// route header.
type peerRouter struct {
	registry *hub.Registry
	metrics  *hubMetrics
}

func (pr peerRouter) RoundTrip(req *http.Request) (*http.Response, error) {
	peer := req.Header.Get(routeHeader)
	if peer == "" {
		// The handler serves a landing page before we get here; this is
		// just defense in depth.
		pr.metrics.recordProxyError(req.Context(), "no-header")

		return nil, notAttachedError{}
	}

	req.Header.Del(routeHeader)

	if !pr.registry.Attached(peer) {
		pr.metrics.recordProxyError(req.Context(), "not-attached")

		return nil, notAttachedError{peer: peer}
	}

	resp, err := pr.registry.RoundTripper(peer).RoundTrip(req)
	if err != nil {
		pr.metrics.recordProxyError(req.Context(), "transport")
	}

	return resp, err
}

// notAttachedError marks "no such live peer" so the error handler can
// answer 404 rather than 502 (the peer is not a failing upstream, it is
// simply absent).
type notAttachedError struct{ peer string }

func (e notAttachedError) Error() string {
	if e.peer == "" {
		return "no target peer named (set the " + routeHeader + " header)"
	}

	return "peer " + strconv.Quote(e.peer) + " is not attached"
}

// proxyError renders a tunnel/proxy failure. An absent peer is a 404,
// not a 502 (it is not a failing upstream, it just is not there), a real
// transport error stays a 502. The body is only the holt swirl, never
// the peer name or any hub detail, so a proxy in front of the hub cannot
// leak anything from an error.
func proxyError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusBadGateway

	var na notAttachedError
	if errors.As(err, &na) {
		status = http.StatusNotFound
	}

	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
		w.Header().Set("Content-Type", "application/grpc")
		w.Header().Set("Grpc-Status", "14") // UNAVAILABLE
		w.Header().Set("Grpc-Message", "unavailable")
		w.WriteHeader(http.StatusOK)

		return
	}

	writeProxyPage(w, r, status)
}

// proxyLanding answers a bare visit to the proxy (no target peer named)
// with the same swirl page instead of a proxied request, so hitting the
// proxy root reveals nothing and never shows a 502.
func proxyLanding(w http.ResponseWriter, r *http.Request) {
	writeProxyPage(w, r, http.StatusBadRequest)
}

// writeProxyPage writes a bare holt swirl, centered, and nothing else,
// so no peer name, address, or other hub state leaks through the proxy,
// whoever the caller is.
func writeProxyPage(w http.ResponseWriter, r *http.Request, status int) {
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, proxyPageHTML)

		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, "🌀\n")
}

// proxyPageHTML is the swirl, centered, self-contained, no other text.
const proxyPageHTML = `<!doctype html><html lang="en"><head><meta charset="utf-8">` +
	`<meta name="viewport" content="width=device-width,initial-scale=1">` +
	`<title>🌀</title><style>` +
	`html,body{height:100%;margin:0}` +
	`body{display:flex;align-items:center;justify-content:center;` +
	`background:#0b0f14;font-size:4rem;line-height:1}` +
	`</style></head><body>🌀</body></html>`

// mintToken issues a JWT for peer and packages a join token — the same
// token `holt enroll` prints, but minted server-side for the
// console's enroll button.
func mintToken(secret []byte, tunnelURL, peer string, ttl time.Duration) (string, error) {
	jwtStr, err := jwtauth.Issue(secret, peer, ttl)
	if err != nil {
		return "", err
	}

	return token.JoinToken{Peer: peer, TunnelURL: tunnelURL, JWT: jwtStr}.Encode(), nil
}

func newH2CServer(handler http.Handler) *http.Server {
	var protocols http.Protocols
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	// ReadHeaderTimeout bounds slow-header (Slowloris) clients;
	// IdleTimeout bounds idle keep-alive connections. No Read/Write
	// timeout: the admin WatchTunnels response and proxied peer
	// responses stream for arbitrary durations.
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
		Protocols:         &protocols,
	}
}

const (
	readHeaderTimeout = 10 * time.Second
	idleTimeout       = 2 * time.Minute
)

func listen(addr string) (net.Listener, error) {
	var lc net.ListenConfig

	return lc.Listen(context.Background(), "tcp", addr)
}

func hostname() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}

	return "hub"
}
