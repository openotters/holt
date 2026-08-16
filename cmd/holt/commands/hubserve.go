package commands

import (
	"context"
	"net"
	"net/http"

	"go.uber.org/zap"

	"github.com/openotters/holt/api/v1/holtv1connect"
	"github.com/openotters/holt/cmd/holt/internal/httpsrv"
	"github.com/openotters/holt/cmd/holt/internal/hubapi"
	"github.com/openotters/holt/cmd/holt/internal/hubmetrics"
	"github.com/openotters/holt/cmd/holt/internal/jwtauth"
	"github.com/openotters/holt/internal/admin"
	"github.com/openotters/holt/internal/attach"
	"github.com/openotters/holt/internal/revproxy"
)

// startServers brings up the tunnel, admin, proxy, and (optional)
// metrics listeners. Each binds before returning, so a taken port fails
// here rather than leaving the hub half up.
func (h *Hub) startServers(ctx context.Context, listeners *httpsrv.Group, rt *hubRuntime) error {
	if err := h.startTunnel(ctx, listeners, rt); err != nil {
		return err
	}

	if err := h.startAdmin(ctx, listeners, rt); err != nil {
		return err
	}

	if err := h.startProxy(ctx, listeners, rt); err != nil {
		return err
	}

	if !h.Metrics {
		return nil
	}

	return h.startMetrics(ctx, listeners)
}

// startTunnel runs the plaintext tunnel listener; peers upgrade to a
// WebSocket that carries the tunnel frames, authenticating with a JWT
// whose subject becomes the tunnel key. A blocked or unroutably-named
// subject is rejected even with a valid token. Transport encryption is
// expected from the network in front of the hub (a TLS edge, ingress,
// or mesh), same as the proxy and admin listeners.
func (h *Hub) startTunnel(ctx context.Context, listeners *httpsrv.Group, rt *hubRuntime) error {
	guard := jwtauth.Guard{
		Secret:   rt.secrets,
		Blocked:  rt.blocks,
		OnReject: rt.metrics.RecordReject,
	}

	// The upgrade is accepted on ANY path, so an advertise URL keeps
	// working whether the ingress in front routes /, a dedicated
	// prefix, or the pre-0.13 gRPC path.
	attach := guard.Middleware(attach.NewHandler(rt.registry, jwtauth.PeerFrom, rt.logger))

	// Optional cap on concurrent tunnel connections. Off by default so
	// behavior is unchanged; a value bounds resource use under a flood
	// of attaches (each tunnel holds an HTTP/2 session).
	return listeners.Start(ctx, "tunnel", h.TunnelAddr, attach, httpsrv.MaxConns(h.MaxConns))
}

// startAdmin runs the Admin gRPC service (list / stop / block), the
// enroll endpoint, and — with --ui — the web console.
func (h *Hub) startAdmin(ctx context.Context, listeners *httpsrv.Group, rt *hubRuntime) error {
	mux := http.NewServeMux()

	adminPath, adminHandler := holtv1connect.NewAdminHandler(
		admin.NewService(rt.registry, admin.WithBlocker(rt.blocks), admin.WithInfo(rt.info)),
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
	hubapi.Enroll{
		Secret:    rt.secrets,
		TunnelURL: h.advertiseURL(),
		TTL:       h.TokenTTL,
	}.Mount(mux)

	if h.UI {
		hubapi.Console{
			State:    h.State,
			Secret:   rt.secrets,
			Tunnels:  rt.registry,
			Settings: h.consoleConfig(),
			Path:     h.UIPath,
			Logger:   rt.logger,
		}.Mount(mux)

		rt.logger.Debug("web console enabled", zap.String("url", "http://"+h.AdminAddr+"/"))
	}

	// The host guard defeats DNS-rebinding against the plaintext
	// console: only loopback, the admin bind host, and any
	// operator-configured hostnames are accepted.
	return listeners.Start(ctx, "admin", h.AdminAddr, httpsrv.HostGuard(h.adminHosts(), mux))
}

// adminHosts is the Host allow-list for the admin listener: whatever
// the operator configured, plus the address the hub binds.
func (h *Hub) adminHosts() []string {
	adminHost := h.AdminAddr
	if host, _, err := net.SplitHostPort(h.AdminAddr); err == nil {
		adminHost = host
	}

	return append([]string{adminHost}, h.AllowedHosts...)
}

// startProxy runs the routed reverse proxy that reaches peer services.
func (h *Hub) startProxy(ctx context.Context, listeners *httpsrv.Group, rt *hubRuntime) error {
	peers := revproxy.New(rt.registry,
		revproxy.WithRouting(h.routing(), h.ProxyDomain),
		revproxy.WithErrorHook(rt.metrics.RecordProxyError),
	)

	return listeners.Start(ctx, "proxy", h.ProxyAddr, rt.metrics.Instrument(peers))
}

// startMetrics serves the OTel Prometheus exporter on /metrics.
func (h *Hub) startMetrics(ctx context.Context, listeners *httpsrv.Group) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", hubmetrics.Handler())

	return listeners.Start(ctx, "metrics", h.MetricsAddr, mux)
}
