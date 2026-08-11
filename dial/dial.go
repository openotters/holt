// Package dial is the client half of the holt: a persistent
// attach loop that dials the hub, serves an http.Handler over the
// reverse tunnel, and redials with jittered backoff until the
// context ends or the hub sends a terminal GoAway.
//
// The loop rides an existing *grpc.ClientConn, so it reuses whatever
// connection (and auth interceptors) the application already holds
// to the hub — the tunnel is one more stream on that connection, not
// a second dial.
package dial

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"time"

	"go.uber.org/zap"
	"golang.org/x/net/http2"
	"google.golang.org/grpc"

	"github.com/openotters/holt"
	holtv1 "github.com/openotters/holt/api/v1"
)

const (
	backoffBase = 500 * time.Millisecond
	backoffCap  = 30 * time.Second

	// readIdleTimeout makes the inner HTTP/2 server ping through the
	// tunnel so a wedged hub-side session is detected end-to-end.
	readIdleTimeout = 30 * time.Second
)

// Options wires one attach loop.
type Options struct {
	// Conn is the peer's existing gRPC connection to the hub. The
	// attach loop opens one Tunnel.Attach stream on it.
	Conn grpc.ClientConnInterface
	// Handler is served over the tunnel — the hub dials it as if it
	// were a normal HTTP server.
	Handler http.Handler
	// Version is the peer build version, sent in Hello (observability).
	Version string
	Logger  *zap.Logger

	// TLSConfig, when set, encrypts the payload end-to-end INSIDE the
	// tunnel: after the plaintext holt handshake the peer runs a
	// TLS server over the stream and serves Handler over HTTPS. The
	// hub must dial with a matching client config (hub.WithPeerTLS).
	// This is independent of any TLS on the outer gRPC connection —
	// it stays encrypted even if that hop is plaintext or terminated
	// at a proxy. NextProtos is forced to h2.
	TLSConfig *tls.Config
}

// Run attaches to the hub and serves Handler over the tunnel,
// redialing with jittered exponential backoff (500 ms doubling to
// 30 s) until ctx is cancelled or the hub sends a terminal GoAway. A
// successful handshake resets the backoff. Returns nil on a terminal
// GoAway (clean exit) and ctx.Err() on shutdown.
func Run(ctx context.Context, opts Options) error {
	logger := opts.Logger.Named("holt-dial")
	backoff := backoffBase

	for {
		attached, err := attachOnce(ctx, opts, logger)

		if ctx.Err() != nil {
			return ctx.Err()
		}

		if reason := holt.GoAwayReason(err); holt.TerminalReason(reason) {
			logger.Info("hub detached the tunnel; not redialing", zap.String("reason", reason))

			return nil
		}

		if attached {
			backoff = backoffBase
		}

		logger.Warn("tunnel detached; redialing",
			zap.Error(err), zap.Duration("backoff", backoff))

		//nolint:gosec // G404: jitter, not crypto
		sleep := backoff/2 + rand.N(backoff/2+1)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleep):
		}

		backoff = min(backoff*2, backoffCap)
	}
}

// attachOnce performs one attach: handshake, then serves Handler
// over the stream until it ends. attached reports whether the
// handshake completed (used to reset backoff).
func attachOnce(ctx context.Context, opts Options, logger *zap.Logger) (bool, error) {
	stream, err := holtv1.NewTunnelClient(opts.Conn).Attach(ctx)
	if err != nil {
		return false, fmt.Errorf("holt: attach: %w", err)
	}

	if hsErr := holt.ClientHandshake(stream, opts.Version); hsErr != nil {
		return false, hsErr
	}

	logger.Info("tunnel attached")

	conn := holt.NewConn(stream,
		holt.WithCloseFunc(stream.CloseSend),
		holt.WithSides("peer", "hub"))

	// Optional payload encryption: wrap the raw tunnel in a TLS
	// server and complete the handshake before serving h2 over it.
	// The handshake is driven explicitly (not lazily by ServeConn) so
	// it rendezvous deterministically with the hub's client-side
	// handshake — ServeConn writes the server preface first, which
	// would otherwise race the TLS record layer. The holt
	// handshake above already completed in plaintext; TLS protects
	// the payload, not the tunnel framing.
	var served net.Conn = conn
	if opts.TLSConfig != nil {
		tlsCfg := opts.TLSConfig.Clone()
		tlsCfg.NextProtos = []string{"h2"}
		tlsConn := tls.Server(conn, tlsCfg)

		if hsErr := tlsConn.HandshakeContext(ctx); hsErr != nil {
			return true, fmt.Errorf("holt: tunnel TLS handshake: %w", hsErr)
		}

		served = tlsConn
	}

	srv := &http2.Server{ReadIdleTimeout: readIdleTimeout}
	srv.ServeConn(served, &http2.ServeConnOpts{
		Context: ctx,
		Handler: opts.Handler,
	})

	if lastErr := conn.LastError(); lastErr != nil {
		return true, lastErr
	}

	return true, errors.New("holt: session ended")
}
