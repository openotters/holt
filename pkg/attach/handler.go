// Package attach is the hub's tunnel front door: an http.Handler that
// accepts a peer's WebSocket upgrade, runs the holt handshake, and
// registers the resulting tunnel in the registry. Identity is the
// application's concern: the Handler is constructed with an Identity
// func that extracts the peer ID from the request context (JWT
// claims, mTLS SAN, header — whatever the surrounding middleware
// established). The handshake itself carries no identity.
package attach

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"golang.org/x/net/http2"

	"github.com/openotters/holt/internal/wire"
	"github.com/openotters/holt/pkg/registry"
)

// pingInterval / pingTimeout drive the inner HTTP/2 session's PING
// through the tunnel — end-to-end liveness that also catches a
// wedged peer whose outer transport is fine.
const (
	pingInterval = 30 * time.Second
	pingTimeout  = 15 * time.Second
)

// Identity extracts the peer ID from a request context. The
// application supplies it — it's the bridge from whatever auth
// middleware wraps the attach handler (JWT claims, mTLS SAN, …) to
// the registry key. Returning ("", err) rejects the attach.
type Identity func(ctx context.Context) (peer string, err error)

// Handler accepts reverse-tunnel attachments over WebSockets: the
// peer upgrades a plain HTTP request, then every binary message
// carries one TunnelFrame. WebSockets are the carrier precisely
// because they pass through the layers that gRPC cannot — CDN
// public hostnames, access proxies, HTTP/1.1-only edges.
//
// Mount it behind the application's auth middleware; the Identity
// func reads whatever that middleware established from the upgrade
// request's context.
type Handler struct {
	registry *registry.Registry
	identity Identity
	logger   *zap.Logger
	peerTLS  *tls.Config
	tracer   trace.Tracer
}

// HandlerOption configures a Handler.
type HandlerOption func(*Handler)

// WithPeerTLS makes the hub run a TLS client over each tunnel before
// speaking HTTP/2, so the payload is encrypted end-to-end between hub
// and peer. Peers must attach with a matching TLS server config
// (dial.Options.TLSConfig). Use it to verify peer certificates and to
// keep the payload confidential even when the outer WebSocket hop is
// plaintext or TLS-terminated at a proxy. NextProtos is forced to h2.
func WithPeerTLS(cfg *tls.Config) HandlerOption {
	return func(h *Handler) { h.peerTLS = cfg }
}

// WithTracerProvider sets the OTel TracerProvider used to span each
// tunnel's lifetime. Optional — without it the global provider is
// used, which is a no-op until the application installs an SDK.
func WithTracerProvider(tp trace.TracerProvider) HandlerOption {
	return func(h *Handler) { h.tracer = tracer(tp) }
}

// NewHandler builds the attach handler. identity maps the
// authenticated request context to the registry key.
func NewHandler(registry *registry.Registry, identity Identity, logger *zap.Logger, opts ...HandlerOption) *Handler {
	h := &Handler{registry: registry, identity: identity, logger: logger.Named("holt-hub")}
	for _, opt := range opts {
		opt(h)
	}

	if h.tracer == nil {
		h.tracer = tracer(nil)
	}

	return h
}

// ServeHTTP accepts one peer's reverse tunnel: identity, WebSocket
// upgrade, handshake, then an HTTP/2 client session over the raw
// byte stream until the peer disconnects or the registry closes the
// tunnel.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	peer, err := h.identity(r.Context())
	if err != nil {
		http.Error(w, "unauthorized: "+err.Error(), http.StatusUnauthorized)

		return
	}

	if peer == "" {
		http.Error(w, "unauthorized: empty peer identity", http.StatusUnauthorized)

		return
	}

	// Peers are programs, not browsers, so the browser same-origin
	// model does not apply; authentication happened above.
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		// Accept has already written the HTTP error response.
		h.logger.Debug("websocket accept failed", zap.String("peer", peer), zap.Error(err))

		return
	}

	h.serve(r.Context(), peer, c)
}

// serve runs one attached tunnel to completion.
func (h *Handler) serve(ctx context.Context, peer string, c *websocket.Conn) {
	// CloseNow is the failure path; the normal path closes below.
	defer func() { _ = c.CloseNow() }()

	ctx, span := h.tracer.Start(ctx, "holt.tunnel",
		trace.WithAttributes(attribute.String("holt.peer", peer)))
	defer span.End()

	fs := wire.NewWSStream(ctx, c)

	hello, hsErr := wire.ServerHandshake(fs)
	if hsErr != nil {
		span.SetStatus(codes.Error, "handshake failed")
		h.logger.Warn("tunnel handshake failed", zap.String("peer", peer), zap.Error(hsErr))

		return
	}

	span.SetAttributes(attribute.String("holt.peer_version", hello.GetPeerVersion()))

	logger := h.logger.With(zap.String("peer", peer))
	logger.Info("tunnel attached")

	closeCtx, closeSession := context.WithCancelCause(ctx)
	defer closeSession(nil)

	conn := wire.NewConn(fs, wire.WithSides("hub", "peer"))
	// Seal the adapter LAST (LIFO): after cc.Close() has flushed its
	// final frames, further transport-goroutine writes fail locally
	// instead of reaching a finished handler's socket.
	defer func() { _ = conn.Close() }()

	cc, err := h.clientConn(ctx, conn)
	if err != nil {
		logger.Warn("tunnel session setup failed", zap.Error(err))

		return
	}
	defer func() { _ = cc.Close() }()

	closeTunnel := func(reason string) {
		_ = conn.SendGoAway(reason)
		closeSession(errors.New(reason))
	}

	detach := h.registry.Attach(peer, hello.GetPeerVersion(), cc, closeTunnel)

	// The ReadIdleTimeout PING closes a dead session, but only the
	// session notices. Poll its state and turn a dead session into a
	// detach so presence reflects reality. Exits with closeCtx.
	go func() {
		ticker := time.NewTicker(pingTimeout)
		defer ticker.Stop()

		for {
			select {
			case <-closeCtx.Done():
				return
			case <-ticker.C:
				if cc.State().Closed {
					closeSession(errors.New("connection-lost"))

					return
				}
			}
		}
	}()

	<-closeCtx.Done()

	reason := "connection-lost"
	if cause := context.Cause(closeCtx); cause != nil && !errors.Is(cause, context.Canceled) {
		reason = cause.Error()
	}

	detach(reason)
	span.SetAttributes(attribute.String("holt.detach_reason", reason))
	logger.Info("tunnel detached", zap.String("reason", reason))

	_ = c.Close(websocket.StatusNormalClosure, reason)
}

// clientConn builds the hub's HTTP/2 client session over the tunnel
// conn, optionally wrapping it in a TLS client first (WithPeerTLS).
// The holt framing handshake has already completed in plaintext on
// conn; TLS, when configured, protects the payload, not the framing.
func (h *Handler) clientConn(ctx context.Context, conn *wire.Conn) (*http2.ClientConn, error) {
	transport := &http2.Transport{
		AllowHTTP:       true,
		ReadIdleTimeout: pingInterval,
		PingTimeout:     pingTimeout,
	}

	var session net.Conn = conn

	if h.peerTLS != nil {
		tlsCfg := h.peerTLS.Clone()
		tlsCfg.NextProtos = []string{"h2"}
		tlsConn := tls.Client(conn, tlsCfg)

		if tlsErr := tlsConn.HandshakeContext(ctx); tlsErr != nil {
			return nil, fmt.Errorf("tunnel TLS handshake: %w", tlsErr)
		}

		session = tlsConn
	}

	cc, err := transport.NewClientConn(session)
	if err != nil {
		return nil, fmt.Errorf("tunnel session setup: %w", err)
	}

	return cc, nil
}

var _ http.Handler = (*Handler)(nil)

// instrumentName is the OTel instrumentation scope for handler spans.
const instrumentName = "github.com/openotters/holt/pkg/attach"

// tracer returns the tracer for handler spans, defaulting to the
// global TracerProvider (no-op until an SDK is installed).
func tracer(tp trace.TracerProvider) trace.Tracer {
	if tp == nil {
		tp = otel.GetTracerProvider()
	}

	return tp.Tracer(instrumentName)
}
