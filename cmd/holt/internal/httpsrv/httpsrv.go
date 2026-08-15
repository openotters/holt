// Package httpsrv is the listener plumbing behind the hub's four
// endpoints (tunnel, admin, proxy, metrics). It gives them one server
// shape — h2c, with the timeouts a public-facing listener needs — one
// place to start them, and one place to drain them on shutdown.
//
// A Group owns the servers it starts, so the hub command reads as the
// list of listeners it brings up:
//
//	g := httpsrv.NewGroup(logger)
//	if err := g.Start(ctx, "tunnel", addr, handler, httpsrv.MaxConns(n)); err != nil {
//		return err
//	}
//	...
//	forced := g.Drain(5 * time.Second)
//
// Start binds synchronously and serves in the background, so a port
// already in use fails at boot with the address in the error, while a
// serve error later in the process's life is logged under the
// listener's name.
//
// Every listener here is plaintext: transport encryption is the
// deployment's job (a TLS edge, ingress, or mesh in front of the hub).
package httpsrv

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"go.uber.org/zap"
	"golang.org/x/net/netutil"
)

const (
	// readHeaderTimeout bounds slow-header (Slowloris) clients.
	readHeaderTimeout = 10 * time.Second
	// idleTimeout bounds idle keep-alive connections.
	idleTimeout = 2 * time.Minute
)

// Group starts HTTP listeners and drains them together. Start it from
// one goroutine (a boot sequence) and Drain it after: the group is not
// safe for concurrent Starts.
type Group struct {
	logger  *zap.Logger
	servers []*http.Server
}

// NewGroup returns an empty group. Serve errors are logged to logger
// under the name each listener was started with.
func NewGroup(logger *zap.Logger) *Group {
	return &Group{logger: logger, servers: nil}
}

// Option adapts a listener before it is served, e.g. to cap connections.
type Option func(net.Listener) net.Listener

// MaxConns caps concurrent connections on the listener. A value of 0 or
// less leaves it uncapped.
func MaxConns(n int) Option {
	return func(lis net.Listener) net.Listener {
		if n <= 0 {
			return lis
		}

		return netutil.LimitListener(lis, n)
	}
}

// Start binds addr and serves handler on it in the background. The bind
// happens before returning, so an address in use is reported here;
// name labels the listener in any later serve error.
func (g *Group) Start(ctx context.Context, name, addr string, handler http.Handler, opts ...Option) error {
	var lc net.ListenConfig

	// net's own error already names the address and the syscall, which
	// is exactly what an operator needs to see for a bind failure.
	lis, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return err
	}

	for _, opt := range opts {
		lis = opt(lis)
	}

	srv := newServer(handler)
	g.servers = append(g.servers, srv)

	go func() {
		if serveErr := srv.Serve(lis); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			g.logger.Error(name+" serve", zap.Error(serveErr))
		}
	}()

	return nil
}

// newServer is the one server shape every hub listener uses: HTTP/1.1
// plus unencrypted HTTP/2 (h2c), which the tunnel's multiplexed
// sessions and the admin gRPC service both need.
//
// No Read/Write timeout: the admin WatchTunnels response and proxied
// peer responses stream for arbitrary durations. ReadHeaderTimeout and
// IdleTimeout bound the two things that would otherwise be unbounded.
func newServer(handler http.Handler) *http.Server {
	var protocols http.Protocols

	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
		Protocols:         &protocols,
	}
}
