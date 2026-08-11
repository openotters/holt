package hub

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"time"

	"connectrpc.com/connect"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"golang.org/x/net/http2"

	"github.com/openotters/holt"
	holtv1 "github.com/openotters/holt/api/v1"
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
// middleware wraps the Attach handler (JWT claims, mTLS SAN, …) to
// the registry key. Returning ("", err) rejects the attach.
type Identity func(ctx context.Context) (peer string, err error)

// Handler implements holtv1connect.TunnelHandler. Mount it
// behind the application's auth middleware; the Identity func reads
// whatever that middleware established.
type Handler struct {
	registry *Registry
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
// keep the payload confidential even when the outer gRPC hop is
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

// NewHandler builds the Attach handler. identity maps the
// authenticated request context to the registry key.
func NewHandler(registry *Registry, identity Identity, logger *zap.Logger, opts ...HandlerOption) *Handler {
	h := &Handler{registry: registry, identity: identity, logger: logger.Named("holt-hub")}
	for _, opt := range opts {
		opt(h)
	}

	if h.tracer == nil {
		h.tracer = tracer(nil)
	}

	return h
}

// Attach accepts one peer's reverse tunnel: handshake, then an
// HTTP/2 client session over the raw byte stream until the peer
// disconnects or the registry closes the tunnel.
func (h *Handler) Attach(
	ctx context.Context,
	stream *connect.BidiStream[holtv1.TunnelFrame, holtv1.TunnelFrame],
) error {
	peer, err := h.identity(ctx)
	if err != nil {
		return connect.NewError(connect.CodeUnauthenticated, err)
	}
	if peer == "" {
		return connect.NewError(connect.CodeUnauthenticated,
			errors.New("holt: empty peer identity"))
	}

	ctx, span := h.tracer.Start(ctx, "holt.tunnel",
		trace.WithAttributes(attribute.String("holt.peer", peer)))
	defer span.End()

	fs := connectStream{stream}

	hello, hsErr := holt.ServerHandshake(fs)
	if hsErr != nil {
		span.SetStatus(codes.Error, "handshake failed")

		return connect.NewError(connect.CodeInvalidArgument, hsErr)
	}

	span.SetAttributes(attribute.String("holt.peer_version", hello.GetPeerVersion()))

	logger := h.logger.With(zap.String("peer", peer))
	logger.Info("tunnel attached")

	closeCtx, closeSession := context.WithCancelCause(ctx)
	defer closeSession(nil)

	conn := holt.NewConn(fs, holt.WithSides("hub", "peer"))
	// Seal the adapter LAST (LIFO): after cc.Close() has flushed its
	// final frames, further transport-goroutine writes fail locally
	// instead of panicking inside a finished connect handler.
	defer func() { _ = conn.Close() }()

	cc, err := h.clientConn(ctx, conn)
	if err != nil {
		return err
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

	return nil
}

// clientConn builds the hub's HTTP/2 client session over the tunnel
// conn, optionally wrapping it in a TLS client first (WithPeerTLS).
// The holt framing handshake has already completed in plaintext on
// conn; TLS, when configured, protects the payload, not the framing.
func (h *Handler) clientConn(ctx context.Context, conn *holt.Conn) (*http2.ClientConn, error) {
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
			return nil, connect.NewError(connect.CodePermissionDenied,
				fmt.Errorf("tunnel TLS handshake: %w", tlsErr))
		}

		session = tlsConn
	}

	cc, err := transport.NewClientConn(session)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal,
			fmt.Errorf("tunnel session setup: %w", err))
	}

	return cc, nil
}

// connectStream adapts connect's BidiStream (Receive) to the
// holt.FrameStream shape (Recv).
type connectStream struct {
	s *connect.BidiStream[holtv1.TunnelFrame, holtv1.TunnelFrame]
}

func (c connectStream) Send(f *holtv1.TunnelFrame) error   { return c.s.Send(f) }
func (c connectStream) Recv() (*holtv1.TunnelFrame, error) { return c.s.Receive() }
