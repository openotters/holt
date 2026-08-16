package holt

import (
	"context"
	"crypto/tls"

	"go.opentelemetry.io/otel/trace"

	"github.com/openotters/holt/internal/tunnel"
	"github.com/openotters/holt/pkg/attach"
)

// Tunnel is the endpoint peers attach to. Build it with NewTunnel.
type Tunnel = tunnel.Tunnel

// TunnelOption configures a Tunnel; every EndpointOption is one too.
type TunnelOption = tunnel.Option

// NewTunnel declares the attach endpoint on addr. Give it an identity
// — WithAuthBearer for bearer tokens, or WithMiddleware + WithIdentity
// for any other scheme — because the peer id is the registry key and
// should come from something verified. Without one, the development
// identity applies: peers name themselves with the x-holt-peer header
// (or get a generated name), nothing verifies the claim, and Run says
// so in the log. Loopback development only.
func NewTunnel(addr string, opts ...TunnelOption) *Tunnel { return tunnel.NewTunnel(addr, opts...) }

// Identity extracts the peer ID from an attach request's context. The
// surrounding middleware establishes it (JWT claims, mTLS SAN, a
// header); returning ("", err) rejects the attach.
type Identity = attach.Identity

// WithIdentity sets how the peer id is derived from the attach
// request's context, after the middleware authenticated it and
// stamped whatever the func reads.
func WithIdentity(identity Identity) TunnelOption { return tunnel.WithIdentity(identity) }

// WithAuthBearer authenticates attaches with a Bearer token: the
// middleware extracts Authorization, asks verify for the peer id it
// proves, answers 401 when it refuses, and wires the identity so the
// registry keys the tunnel by that id. It is WithMiddleware +
// WithIdentity fused for the most common scheme; bring your own pair
// for anything else.
func WithAuthBearer(verify func(ctx context.Context, token string) (peer string, err error)) TunnelOption {
	return tunnel.WithAuthBearer(verify)
}

// HandlerOption configures the attach handler; pass them through
// WithHandlerOptions.
type HandlerOption = attach.HandlerOption

// WithHandlerOptions passes options through to the attach handler —
// WithPeerTLS for inner TLS, WithTracerProvider for tracing.
func WithHandlerOptions(opts ...HandlerOption) TunnelOption {
	return tunnel.WithHandlerOptions(opts...)
}

// WithPeerTLS makes the hub run TLS INSIDE each tunnel with this
// client config after the plaintext holt handshake — pairing with the
// peer's WithTunnelTLS, so the payload stays encrypted end-to-end
// even across a plaintext or TLS-terminated transport.
func WithPeerTLS(cfg *tls.Config) HandlerOption { return attach.WithPeerTLS(cfg) }

// WithTracerProvider sets the OTel tracer provider for attach spans.
// Default: the global provider.
func WithTracerProvider(tp trace.TracerProvider) HandlerOption {
	return attach.WithTracerProvider(tp)
}

// DevPeerHeader is how a peer names itself under the development
// identity — the default when a tunnel has no identity configured.
// The claim is not verified.
const DevPeerHeader = tunnel.DevPeerHeader
