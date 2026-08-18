// Package tunnel declares the endpoint peers attach to: where it
// serves, how the peer id is derived, and what passes through to the
// attach handler.
package tunnel

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/openotters/holt/internal/utils"
	"github.com/openotters/holt/pkg/attach"
)

// Tunnel is the endpoint peers attach to. Build it with NewTunnel.
type Tunnel struct {
	utils.Endpoint

	Identity    attach.Identity
	HandlerOpts []attach.HandlerOption
}

// Option configures a Tunnel; every EndpointOption is one too.
type Option interface{ ApplyTunnel(*Tunnel) }

type tunnelOption func(*Tunnel)

func (f tunnelOption) ApplyTunnel(t *Tunnel) { f(t) }

// NewTunnel declares the attach endpoint on addr. Give it an identity
// — WithAuthBearer for bearer tokens, or WithMiddleware + WithIdentity
// for any other scheme — because the peer id is the registry key and
// should come from something verified. Without one, the development
// identity applies: peers name themselves with the x-holt-peer header
// (or get a generated name), nothing verifies the claim, and Run says
// so in the log. Loopback development only.
func NewTunnel(addr string, opts ...Option) *Tunnel {
	t := &Tunnel{Endpoint: utils.Endpoint{Addr: addr}}

	for _, opt := range opts {
		opt.ApplyTunnel(t)
	}

	return t
}

// WithIdentity sets how the peer id is derived from the attach
// request's context, after the middleware authenticated it and
// stamped whatever the func reads.
func WithIdentity(identity attach.Identity) Option {
	return tunnelOption(func(t *Tunnel) { t.Identity = identity })
}

// WithHandlerOptions passes options through to the attach handler —
// WithPeerTLS for inner TLS, WithTracerProvider for tracing.
func WithHandlerOptions(opts ...attach.HandlerOption) Option {
	return tunnelOption(func(t *Tunnel) { t.HandlerOpts = append(t.HandlerOpts, opts...) })
}

// WithAuthBearer authenticates attaches with a Bearer token: the
// middleware extracts Authorization, asks verify for the peer id it
// proves, answers 401 when it refuses, and wires the identity so the
// registry keys the tunnel by that id. It is WithMiddleware +
// WithIdentity fused for the most common scheme; bring your own pair
// for anything else.
func WithAuthBearer(verify func(ctx context.Context, token string) (peer string, err error)) Option {
	return tunnelOption(func(t *Tunnel) {
		t.Middleware = append(t.Middleware, bearerMiddleware(verify))
		t.Identity = bearerIdentity
	})
}

// BoundName names the tunnel's bind for the loopback error message.
func (t *Tunnel) BoundName() string {
	if t.Lis != nil {
		return t.Lis.Addr().String()
	}

	return t.Addr
}

type bearerPeerKey struct{}

// bearerMiddleware is the auth half of WithAuthBearer.
func bearerMiddleware(verify func(ctx context.Context, token string) (string, error)) utils.Middleware {
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

type devPeerKey struct{}

// DevIdentity is the zero-configuration identity: the x-holt-peer
// header when the peer sent one, a generated peer-N otherwise.
// Nothing verifies either, which is why installing it is worth a
// warning in the log — it exists so NewServer().Run(ctx) works on
// loopback with no ceremony at all.
type DevIdentity struct {
	counter atomic.Int64
}

// Middleware resolves the peer name onto the request context.
func (d *DevIdentity) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer := r.Header.Get(DevPeerHeader)
		if peer == "" {
			peer = fmt.Sprintf("peer-%d", d.counter.Add(1))
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), devPeerKey{}, peer)))
	})
}

// Identity reads the name the middleware resolved.
func (d *DevIdentity) Identity(ctx context.Context) (string, error) {
	peer, _ := ctx.Value(devPeerKey{}).(string)
	if peer == "" {
		return "", errors.New("unauthenticated")
	}

	return peer, nil
}
