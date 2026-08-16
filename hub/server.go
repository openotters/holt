package hub

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/openotters/holt/hub/proxy"
)

// Server is the assembled hub: a Registry, a Tunnel endpoint peers
// attach to, and (optionally) a Proxy endpoint that reaches them. It
// is the one-call counterpart of dial.Run for the server side — the
// pieces underneath (NewRegistry, NewHandler, proxy.New) stay public
// for applications that want to mount them on their own routers.
//
//	srv := hub.NewServer(
//		hub.WithLogger(logger),
//		hub.WithTunnel(hub.NewTunnel(":7000",
//			hub.WithAuthBearer(peerForToken), // token → peer id
//		)),
//		hub.WithProxy(hub.NewProxy(":7002")), // reach peers: x-tunnel-peer
//	)
//
//	err := srv.Run(ctx) // binds, serves, blocks; cancel ctx to drain
//
// Zero configuration works: hub.NewServer().Run(ctx) serves a tunnel
// on 127.0.0.1:7000 and a proxy on 127.0.0.1:7002 with the
// development identity — peers name themselves with the x-holt-peer
// header (or get a generated name) and nothing verifies the claim.
// That identity is loopback-only by construction: a tunnel bound to
// anything other than a loopback address with no identity configured
// refuses to start, so the trusting default cannot reach a network.
//
// The operator surface stays reachable through Registry: roster,
// stop, watch, or mounting the hub/admin service.
type Server struct {
	logger   *zap.Logger
	registry *Registry
	grace    time.Duration

	tunnel *Tunnel
	proxyd *Proxy
}

// The addresses NewServer serves on when WithTunnel / WithProxy are
// not given — holt's canonical loopback ports.
const (
	DefaultTunnelAddr = "127.0.0.1:7000"
	DefaultProxyAddr  = "127.0.0.1:7002"
)

// Option configures a Server.
type Option func(*Server)

// WithLogger sets the logger. Default: no logging.
func WithLogger(logger *zap.Logger) Option {
	return func(s *Server) { s.logger = logger }
}

// WithRegistry supplies the registry instead of the default
// NewRegistry(logger) — the way to set a hub id, a shared presence
// Directory, or a MeterProvider.
func WithRegistry(registry *Registry) Option {
	return func(s *Server) { s.registry = registry }
}

// WithTunnel serves the given tunnel endpoint instead of the default
// one on DefaultTunnelAddr (see NewTunnel). Nil disables the tunnel
// entirely, for a proxy-only process.
func WithTunnel(t *Tunnel) Option {
	return func(s *Server) { s.tunnel = t }
}

// WithProxy serves the given proxy endpoint instead of the default
// one on DefaultProxyAddr (see NewProxy). Nil disables the proxy
// entirely, for a tunnel-only process.
func WithProxy(p *Proxy) Option {
	return func(s *Server) { s.proxyd = p }
}

// WithGrace bounds how long Run waits for in-flight work when
// draining. Default 5s.
func WithGrace(grace time.Duration) Option {
	return func(s *Server) { s.grace = grace }
}

// defaultGrace bounds the drain when WithGrace is not given.
const defaultGrace = 5 * time.Second

// NewServer assembles a hub from the options, laid over the defaults
// — the caller's options are just applied last, so overriding a
// default is passing the same option with another value. Nothing
// binds or serves until Run.
func NewServer(opts ...Option) *Server {
	s := &Server{}

	defaults := []Option{
		WithLogger(zap.NewNop()),
		WithGrace(defaultGrace),
		WithTunnel(NewTunnel(DefaultTunnelAddr)),
		WithProxy(NewProxy(DefaultProxyAddr)),
	}

	for _, opt := range append(defaults, opts...) {
		opt(s)
	}

	// The default registry is built last: it needs the resolved logger.
	if s.registry == nil {
		s.registry = NewRegistry(s.logger)
	}

	return s
}

// Registry is the operational surface over the server's live tunnels:
// roster, stop, watch, and the value to build a hub/admin service on.
func (s *Server) Registry() *Registry { return s.registry }

// Middleware wraps an endpoint's handler — auth on the tunnel,
// instrumentation on the proxy, anything http.
type Middleware func(http.Handler) http.Handler

// endpoint is what Tunnel and Proxy share: where to serve (an address
// to bind, or a listener the caller already bound — TLS, a systemd
// socket, ":0" in a test) and the middleware around the handler.
type endpoint struct {
	addr       string
	lis        net.Listener
	middleware []Middleware
}

func (e endpoint) configured() bool { return e.addr != "" || e.lis != nil }

// wrap applies the middleware, first-listed outermost.
func (e endpoint) wrap(handler http.Handler) http.Handler {
	for i := len(e.middleware) - 1; i >= 0; i-- {
		handler = e.middleware[i](handler)
	}

	return handler
}

// EndpointOption configures either endpoint kind: it is accepted by
// both NewTunnel and NewProxy.
type EndpointOption interface {
	TunnelOption
	ProxyOption
}

// endpointOption implements EndpointOption over the shared config.
type endpointOption func(*endpoint)

func (f endpointOption) applyTunnel(t *Tunnel) { f(&t.endpoint) }
func (f endpointOption) applyProxy(p *Proxy)   { f(&p.endpoint) }

// WithListener serves the endpoint on a listener the caller bound —
// a tls.Listener for wss, a systemd socket, ":0" in a test — instead
// of binding the endpoint's address. The listener is closed when Run
// returns.
func WithListener(lis net.Listener) EndpointOption {
	return endpointOption(func(e *endpoint) { e.lis = lis })
}

// WithMiddleware wraps the endpoint's handler, first-listed
// outermost. Repeatable; later calls append.
func WithMiddleware(middleware ...Middleware) EndpointOption {
	return endpointOption(func(e *endpoint) { e.middleware = append(e.middleware, middleware...) })
}

// Tunnel is the endpoint peers attach to. Build it with NewTunnel.
type Tunnel struct {
	endpoint

	identity    Identity
	handlerOpts []HandlerOption
}

// TunnelOption configures a Tunnel; every EndpointOption is one too.
type TunnelOption interface{ applyTunnel(*Tunnel) }

// tunnelOption implements TunnelOption.
type tunnelOption func(*Tunnel)

func (f tunnelOption) applyTunnel(t *Tunnel) { f(t) }

// NewTunnel declares the attach endpoint on addr. Give it an identity
// — WithAuthBearer for bearer tokens, or WithMiddleware + WithIdentity
// for any other scheme — because the peer id is the registry key and
// should come from something verified. Without one, the development
// identity applies: peers name themselves with the x-holt-peer header
// (or get a generated name), nothing verifies the claim, and Run says
// so in the log. Loopback development only.
func NewTunnel(addr string, opts ...TunnelOption) *Tunnel {
	t := &Tunnel{endpoint: endpoint{addr: addr}}

	for _, opt := range opts {
		opt.applyTunnel(t)
	}

	return t
}

// WithIdentity sets how the peer id is derived from the attach
// request's context, after the middleware authenticated it and
// stamped whatever the func reads.
func WithIdentity(identity Identity) TunnelOption {
	return tunnelOption(func(t *Tunnel) { t.identity = identity })
}

// WithHandlerOptions passes options through to the attach handler —
// WithPeerTLS for inner TLS, WithTracerProvider for tracing.
func WithHandlerOptions(opts ...HandlerOption) TunnelOption {
	return tunnelOption(func(t *Tunnel) { t.handlerOpts = append(t.handlerOpts, opts...) })
}

// WithAuthBearer authenticates attaches with a Bearer token: the
// middleware extracts Authorization, asks verify for the peer id it
// proves, answers 401 when it refuses, and wires the identity so the
// registry keys the tunnel by that id. It is WithMiddleware +
// WithIdentity fused for the most common scheme; bring your own pair
// for anything else.
func WithAuthBearer(verify func(ctx context.Context, token string) (peer string, err error)) TunnelOption {
	return tunnelOption(func(t *Tunnel) {
		t.middleware = append(t.middleware, bearerMiddleware(verify))
		t.identity = bearerIdentity
	})
}

// bearerPeerKey carries the peer id WithAuthBearer verified.
type bearerPeerKey struct{}

// bearerMiddleware is the auth half of WithAuthBearer.
func bearerMiddleware(verify func(ctx context.Context, token string) (string, error)) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || token == "" {
				http.Error(w, "unauthorized: missing bearer token", http.StatusUnauthorized)

				return
			}

			peer, err := verify(r.Context(), token)
			if err != nil {
				http.Error(w, "unauthorized: "+err.Error(), http.StatusUnauthorized)

				return
			}

			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), bearerPeerKey{}, peer)))
		})
	}
}

// bearerIdentity is the identity half of WithAuthBearer.
func bearerIdentity(ctx context.Context) (string, error) {
	peer, _ := ctx.Value(bearerPeerKey{}).(string)
	if peer == "" {
		return "", errors.New("unauthenticated")
	}

	return peer, nil
}

// DevPeerHeader is how a peer names itself under the development
// identity — the default when a tunnel has no identity configured.
// The claim is not verified.
const DevPeerHeader = "x-holt-peer"

// devPeerKey carries the name the development identity resolved.
type devPeerKey struct{}

// devIdentity is the zero-configuration identity: the x-holt-peer
// header when the peer sent one, a generated peer-N otherwise.
// Nothing verifies either, which is why installing it is worth a
// warning in the log — it exists so NewServer().Run(ctx) works on
// loopback with no ceremony at all.
type devIdentity struct {
	counter atomic.Int64
}

// middleware resolves the peer name onto the request context.
func (d *devIdentity) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer := r.Header.Get(DevPeerHeader)
		if peer == "" {
			peer = fmt.Sprintf("peer-%d", d.counter.Add(1))
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), devPeerKey{}, peer)))
	})
}

// identity reads the name the middleware resolved.
func (d *devIdentity) identity(ctx context.Context) (string, error) {
	peer, _ := ctx.Value(devPeerKey{}).(string)
	if peer == "" {
		return "", errors.New("unauthenticated")
	}

	return peer, nil
}

// Proxy is the endpoint that reaches peers through their tunnels.
// Build it with NewProxy.
type Proxy struct {
	endpoint

	opts []proxy.Option
}

// ProxyOption configures a Proxy; every EndpointOption is one too.
type ProxyOption interface{ applyProxy(*Proxy) }

// proxyOption implements ProxyOption.
type proxyOption func(*Proxy)

func (f proxyOption) applyProxy(p *Proxy) { f(p) }

// NewProxy declares the peer-reaching reverse proxy on addr. By
// default it routes on the x-tunnel-peer header; WithRouting adds
// subdomain routing, WithErrorHook observability.
func NewProxy(addr string, opts ...ProxyOption) *Proxy {
	p := &Proxy{endpoint: endpoint{addr: addr}}

	for _, opt := range opts {
		opt.applyProxy(p)
	}

	return p
}

// WithRouting sets how the proxy picks the target peer; domain is the
// base domain for the subdomain strategies. See proxy.Routing.
func WithRouting(routing proxy.Routing, domain string) ProxyOption {
	return proxyOption(func(p *Proxy) { p.opts = append(p.opts, proxy.WithRouting(routing, domain)) })
}

// WithErrorHook observes requests the proxy could not serve, with a
// low-cardinality reason. See proxy.ErrorHook.
func WithErrorHook(hook proxy.ErrorHook) ProxyOption {
	return proxyOption(func(p *Proxy) { p.opts = append(p.opts, proxy.WithErrorHook(hook)) })
}

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
	hasTunnel := s.tunnel != nil && s.tunnel.configured()
	hasProxy := s.proxyd != nil && s.proxyd.configured()

	if !hasTunnel && !hasProxy {
		return errors.New("holt: nothing to serve; configure WithTunnel and/or WithProxy")
	}

	// The development identity trusts what peers claim, so it only
	// covers a tunnel nothing beyond this machine can reach. A wider
	// bind with no identity is refused rather than served trusting.
	if hasTunnel && s.tunnel.identity == nil && !loopbackOnly(s.tunnel.endpoint) {
		return fmt.Errorf(
			"holt: the tunnel binds %q with no identity configured; the development identity is loopback-only"+
				" — bind 127.0.0.1, or configure WithAuthBearer / WithIdentity", s.tunnel.boundName())
	}

	return nil
}

// boundName names the tunnel's bind for the loopback error message.
func (t *Tunnel) boundName() string {
	if t.lis != nil {
		return t.lis.Addr().String()
	}

	return t.addr
}

// loopbackOnly reports whether the endpoint can only be reached from
// this machine. Anything unparseable counts as reachable, so the
// development identity never engages on a bind it cannot vouch for.
func loopbackOnly(e endpoint) bool {
	host := ""

	if e.lis != nil {
		host, _, _ = net.SplitHostPort(e.lis.Addr().String())
	} else if h, _, err := net.SplitHostPort(e.addr); err == nil {
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

	if s.tunnel != nil && s.tunnel.configured() {
		identity := s.tunnel.identity
		middleware := s.tunnel.wrap

		// No identity configured: the development identity fills in so
		// zero configuration serves, and the log says what that means.
		if identity == nil {
			dev := &devIdentity{}
			identity = dev.identity
			middleware = func(h http.Handler) http.Handler { return s.tunnel.wrap(dev.middleware(h)) }

			s.logger.Warn("holt tunnel has no identity configured; using the development identity" +
				" — peers name themselves (" + DevPeerHeader + ", unverified)." +
				" Loopback development only: configure WithAuthBearer or WithIdentity before exposing this")
		}

		handler := middleware(NewHandler(s.registry, identity, s.logger, s.tunnel.handlerOpts...))

		srv, err := s.serve(ctx, "tunnel", s.tunnel.endpoint, handler)
		if err != nil {
			return nil, err
		}

		servers = append(servers, srv)
	}

	if s.proxyd != nil && s.proxyd.configured() {
		handler := s.proxyd.wrap(proxy.New(s.registry, s.proxyd.opts...))

		srv, err := s.serve(ctx, "proxy", s.proxyd.endpoint, handler)
		if err != nil {
			closeAll(servers)

			return nil, err
		}

		servers = append(servers, srv)
	}

	return servers, nil
}

// serve binds the endpoint (unless the caller brought a listener) and
// serves handler on it in the background. name labels a later serve
// error in the log.
func (s *Server) serve(ctx context.Context, name string, e endpoint, handler http.Handler) (*http.Server, error) {
	lis := e.lis
	if lis == nil {
		var lc net.ListenConfig

		bound, err := lc.Listen(ctx, "tcp", e.addr)
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
