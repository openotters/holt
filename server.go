package holt

import (
	"time"

	"github.com/openotters/holt/internal/registry"
	"github.com/openotters/holt/internal/server"
)

// Server is the assembled hub: a Registry, a Tunnel endpoint peers
// attach to, and (optionally) a Proxy endpoint that reaches them. It
// is the one-call counterpart of NewClient for the server side.
//
//	srv := holt.NewServer(
//		holt.WithLogger(logger),
//		holt.WithTunnel(holt.NewTunnel(":7000",
//			holt.WithAuthBearer(peerForToken), // token → peer id
//		)),
//		holt.WithProxy(holt.NewProxy(":7002")), // reach peers: x-tunnel-peer
//	)
//
//	err := srv.Run(ctx) // binds, serves, blocks; cancel ctx to drain
//
// Zero configuration works: holt.NewServer().Run(ctx) serves a tunnel
// on 127.0.0.1:7000 and a proxy on 127.0.0.1:7002 with the
// development identity — peers name themselves with the x-holt-peer
// header (or get a generated name) and nothing verifies the claim.
// That identity is loopback-only by construction: a tunnel bound to
// anything other than a loopback address with no identity configured
// refuses to start, so the trusting default cannot reach a network.
//
// The operator surface stays reachable through Registry: roster,
// stop, watch.
type Server = server.Server

// The addresses NewServer serves on when WithTunnel / WithProxy are
// not given — holt's canonical loopback ports.
const (
	DefaultTunnelAddr = server.DefaultTunnelAddr
	DefaultProxyAddr  = server.DefaultProxyAddr
)

// Option configures a Server; every SharedOption is one too.
type Option = server.Option

// NewServer assembles a hub from the options, laid over the defaults
// — the caller's options are just applied last, so overriding a
// default is passing the same option with another value. Nothing
// binds or serves until Run.
func NewServer(opts ...Option) *Server { return server.New(opts...) }

// WithRegistry supplies the registry instead of the default
// NewRegistry(logger) — the way to set a hub id, a shared presence
// Directory, or a MeterProvider.
func WithRegistry(reg *registry.Registry) Option { return server.WithRegistry(reg) }

// WithTunnel serves the given tunnel endpoint instead of the default
// one on DefaultTunnelAddr (see NewTunnel). Nil disables the tunnel
// entirely, for a proxy-only process.
func WithTunnel(t *Tunnel) Option { return server.WithTunnel(t) }

// WithProxy serves the given proxy endpoint instead of the default
// one on DefaultProxyAddr (see NewProxy). Nil disables the proxy
// entirely, for a tunnel-only process.
func WithProxy(p *Proxy) Option { return server.WithProxy(p) }

// WithGrace bounds how long Run waits for in-flight work when
// draining. Default 5s.
func WithGrace(grace time.Duration) Option { return server.WithGrace(grace) }
