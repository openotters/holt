// Package server assembles the hub half: a registry, a tunnel
// endpoint peers attach to, and (optionally) a proxy endpoint that
// reaches them, served together under one lifecycle.
package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/openotters/holt/internal/proxy"
	"github.com/openotters/holt/internal/tunnel"
	"github.com/openotters/holt/internal/utils"
	"github.com/openotters/holt/pkg/attach"
	"github.com/openotters/holt/pkg/registry"
	"github.com/openotters/holt/pkg/revproxy"
)

// Server is the assembled hub: a Registry, a Tunnel endpoint peers
// attach to, and (optionally) a Proxy endpoint that reaches them. It
// is the one-call counterpart of client.New for the server side.
//
// Zero configuration works: New().Run(ctx) serves a tunnel on
// 127.0.0.1:7000 and a proxy on 127.0.0.1:7002 with the development
// identity — peers name themselves with the x-holt-peer header (or
// get a generated name) and nothing verifies the claim. That identity
// is loopback-only by construction: a tunnel bound to anything other
// than a loopback address with no identity configured refuses to
// start, so the trusting default cannot reach a network.
//
// The operator surface stays reachable through Registry: roster,
// stop, watch, or mounting the admin service.
type Server struct {
	logger   *zap.Logger
	registry *registry.Registry
	grace    time.Duration

	tunnel *tunnel.Tunnel
	proxyd *proxy.Proxy
}

// The addresses New serves on when WithTunnel / WithProxy are not
// given — holt's canonical loopback ports.
const (
	DefaultTunnelAddr = "127.0.0.1:7000"
	DefaultProxyAddr  = "127.0.0.1:7002"
)

// Option configures a Server; every SharedOption is one too.
type Option interface{ ApplyServer(*Server) }

// serverOption implements Option.
type serverOption func(*Server)

func (f serverOption) ApplyServer(s *Server) { f(s) }

// WithLogger sets the logger. Default: no logging.
func WithLogger(logger *zap.Logger) Option {
	return serverOption(func(s *Server) { s.logger = logger })
}

// WithRegistry supplies the registry instead of the default
// registry.NewRegistry(logger) — the way to set a hub id, a shared
// presence Directory, or a MeterProvider.
func WithRegistry(reg *registry.Registry) Option {
	return serverOption(func(s *Server) { s.registry = reg })
}

// WithTunnel serves the given tunnel endpoint instead of the default
// one on DefaultTunnelAddr (see tunnel.NewTunnel). Nil disables the
// tunnel entirely, for a proxy-only process.
func WithTunnel(t *tunnel.Tunnel) Option {
	return serverOption(func(s *Server) { s.tunnel = t })
}

// WithProxy serves the given proxy endpoint instead of the default
// one on DefaultProxyAddr (see proxy.NewProxy). Nil disables the
// proxy entirely, for a tunnel-only process.
func WithProxy(p *proxy.Proxy) Option {
	return serverOption(func(s *Server) { s.proxyd = p })
}

// WithGrace bounds how long Run waits for in-flight work when
// draining. Default 5s.
func WithGrace(grace time.Duration) Option {
	return serverOption(func(s *Server) { s.grace = grace })
}

// defaultGrace bounds the drain when WithGrace is not given.
const defaultGrace = 5 * time.Second

// New assembles a hub from the options, laid over the defaults — the
// caller's options are just applied last, so overriding a default is
// passing the same option with another value. Nothing binds or serves
// until Run.
func New(opts ...Option) *Server {
	s := &Server{}

	defaults := []Option{
		WithLogger(zap.NewNop()),
		WithGrace(defaultGrace),
		WithTunnel(tunnel.NewTunnel(DefaultTunnelAddr)),
		WithProxy(proxy.NewProxy(DefaultProxyAddr)),
	}

	for _, opt := range append(defaults, opts...) {
		opt.ApplyServer(s)
	}

	// The default registry is built last: it needs the resolved logger.
	if s.registry == nil {
		s.registry = registry.NewRegistry(s.logger)
	}

	return s
}

// Registry is the operational surface over the server's live tunnels:
// roster, stop, watch, and the value to build an admin service on.
func (s *Server) Registry() *registry.Registry { return s.registry }

// Run binds the configured endpoints, serves, and blocks until ctx is
// cancelled — then stops every tunnel, drains the listeners for up to
// the grace period, and returns nil. Binding is synchronous, so a
// taken port fails here, before anything serves.
func (s *Server) Run(ctx context.Context) error {
	if err := s.validate(); err != nil {
		return err
	}

	servers, err := s.start(ctx)
	if err != nil {
		return err
	}

	<-ctx.Done()

	s.logger.Info("holt hub draining",
		zap.Int("tunnels", s.registry.CountTunnels()), zap.Duration("grace", s.grace))
	s.registry.StopAllTunnels("shutting-down")

	// The run context is already cancelled; the drain gets its own
	// deadline so in-flight requests can finish.
	drainCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.grace)
	defer cancel()

	for _, srv := range servers {
		if shutdownErr := srv.Shutdown(drainCtx); shutdownErr != nil {
			_ = srv.Close()
		}
	}

	return nil
}

// validate rejects configurations that could not serve, before any
// port is bound.
func (s *Server) validate() error {
	hasTunnel := s.tunnel != nil && s.tunnel.Configured()
	hasProxy := s.proxyd != nil && s.proxyd.Configured()

	if !hasTunnel && !hasProxy {
		return errors.New("holt: nothing to serve; configure WithTunnel and/or WithProxy")
	}

	// The development identity trusts what peers claim, so it only
	// covers a tunnel nothing beyond this machine can reach. A wider
	// bind with no identity is refused rather than served trusting.
	if hasTunnel && s.tunnel.Identity == nil && !loopbackOnly(s.tunnel.Endpoint) {
		return fmt.Errorf(
			"holt: the tunnel binds %q with no identity configured; the development identity is loopback-only"+
				" — bind 127.0.0.1, or configure WithAuthBearer / WithIdentity", s.tunnel.BoundName())
	}

	// A routing strategy that cannot resolve (unknown, or a mismatched
	// domain) is refused here rather than served routing nothing.
	if hasProxy && s.proxyd.Routing != "" {
		if _, err := s.proxyd.Routing.Resolvers(s.proxyd.Domain); err != nil {
			return fmt.Errorf("holt: %w", err)
		}
	}

	return nil
}

// loopbackOnly reports whether the endpoint can only be reached from
// this machine. Anything unparseable counts as reachable, so the
// development identity never engages on a bind it cannot vouch for.
func loopbackOnly(e utils.Endpoint) bool {
	host := ""

	if e.Lis != nil {
		host, _, _ = net.SplitHostPort(e.Lis.Addr().String())
	} else if h, _, err := net.SplitHostPort(e.Addr); err == nil {
		host = h
	}

	if host == "localhost" {
		return true
	}

	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
}

// start binds and serves every configured endpoint, closing whatever
// was already bound if a later bind fails, so Run never leaves a
// half-listening process behind.
func (s *Server) start(ctx context.Context) ([]*http.Server, error) {
	var servers []*http.Server

	if s.tunnel != nil && s.tunnel.Configured() {
		identity := s.tunnel.Identity
		middleware := s.tunnel.Wrap

		// No identity configured: the development identity fills in so
		// zero configuration serves, and the log says what that means.
		if identity == nil {
			dev := &tunnel.DevIdentity{}
			identity = dev.Identity
			middleware = func(h http.Handler) http.Handler { return s.tunnel.Wrap(dev.Middleware(h)) }

			s.logger.Warn("holt tunnel has no identity configured; using the development identity" +
				" — peers name themselves (" + tunnel.DevPeerHeader + ", unverified)." +
				" Loopback development only: configure WithAuthBearer or WithIdentity before exposing this")
		}

		handler := middleware(attach.NewHandler(s.registry, identity, s.logger, s.tunnel.HandlerOpts...))

		srv, err := s.serve(ctx, "tunnel", s.tunnel.Endpoint, handler)
		if err != nil {
			return nil, err
		}

		servers = append(servers, srv)
	}

	if s.proxyd != nil && s.proxyd.Configured() {
		handler, err := s.proxyHandler()
		if err != nil {
			closeAll(servers)

			return nil, err
		}

		srv, err := s.serve(ctx, "proxy", s.proxyd.Endpoint, handler)
		if err != nil {
			closeAll(servers)

			return nil, err
		}

		servers = append(servers, srv)
	}

	return servers, nil
}

// proxyHandler builds the data plane from the endpoint's declaration.
// The resolver chain puts the configured strategy's resolvers first
// (validate already rejected an unresolvable pair), then the custom
// ones; with neither, the revproxy default (header routing) applies.
func (s *Server) proxyHandler() (http.Handler, error) {
	var chain []revproxy.Resolver

	if s.proxyd.Routing != "" {
		resolvers, err := s.proxyd.Routing.Resolvers(s.proxyd.Domain)
		if err != nil {
			return nil, fmt.Errorf("holt: %w", err)
		}

		chain = append(chain, resolvers...)
	}

	chain = append(chain, s.proxyd.Resolvers...)

	opts := s.proxyd.Opts
	if len(chain) > 0 {
		opts = append(opts, revproxy.WithResolvers(chain...))
	}

	return s.proxyd.Wrap(revproxy.New(s.registry, opts...)), nil
}

// serve binds the endpoint (unless the caller brought a listener) and
// serves handler on it in the background. name labels a later serve
// error in the log.
func (s *Server) serve(ctx context.Context, name string, e utils.Endpoint, handler http.Handler) (*http.Server, error) {
	lis := e.Lis
	if lis == nil {
		var lc net.ListenConfig

		bound, err := lc.Listen(ctx, "tcp", e.Addr)
		if err != nil {
			return nil, fmt.Errorf("holt: %s: %w", name, err)
		}

		lis = bound
	}

	srv := newServer(handler)

	go func() {
		if err := srv.Serve(lis); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("holt "+name+" serve", zap.Error(err))
		}
	}()

	s.logger.Info("holt "+name+" up", zap.String("addr", lis.Addr().String()))

	return srv, nil
}

// newServer is the shape both endpoints share: HTTP/1.1 for the
// WebSocket upgrade, HTTP/2 (including h2c) so gRPC can pass through
// the proxy. No read/write timeout — tunnels and proxied responses
// stream for arbitrary durations — but slow headers and idle
// keep-alives are bounded.
func newServer(handler http.Handler) *http.Server {
	var protocols http.Protocols

	protocols.SetHTTP1(true)
	protocols.SetHTTP2(true)
	protocols.SetUnencryptedHTTP2(true)

	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		Protocols:         &protocols,
	}
}

// closeAll force-closes servers that were started before a later bind
// failed.
func closeAll(servers []*http.Server) {
	for _, srv := range servers {
		_ = srv.Close()
	}
}
