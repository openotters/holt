package jwtauth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/openotters/holt/pkg/peername"
)

// Blocker is the peer-id denylist the guard consults on every attach.
// The ban is on the identity, not one token, so a blocked peer stays
// out even holding a valid, unexpired token.
type Blocker interface {
	IsBlocked(peer string) bool
}

// RejectHook observes refused attaches with a low-cardinality reason,
// which makes it a metric counter's attribute as-is.
type RejectHook func(ctx context.Context, reason string)

// The reasons a RejectHook is called with.
const (
	// ReasonUnauthorized is a missing, malformed, expired, or
	// wrongly-signed token.
	ReasonUnauthorized = "unauthorized"
	// ReasonInvalidName is a valid token naming a peer the proxy could
	// never address.
	ReasonInvalidName = "invalid-peer-name"
	// ReasonBlocked is a valid token for a denylisted peer.
	ReasonBlocked = "blocked"
)

// Guard is the attach-time gate in front of the hub's tunnel handler:
// it verifies the Bearer JWT, refuses unroutable and blocked peers, and
// stamps the peer id onto the request context for PeerFrom.
//
//	guard := jwtauth.Guard{Secret: secret, Blocked: blocks, OnReject: metrics.RecordReject}
//	mux.Handle("/", guard.Middleware(attach.NewHandler(registry, jwtauth.PeerFrom, logger)))
//
// Only Secret is required: without a Blocker nothing is denylisted
// (every valid token attaches), and without a RejectHook refusals are
// simply not counted.
type Guard struct {
	Secret   *Secret
	Blocked  Blocker
	OnReject RejectHook
}

// Middleware wraps next with the attach-time checks. The secret is read
// per request, so a rotate takes effect on the next attach.
func (g Guard) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")

		peer, err := Verify(g.Secret.Get(), bearer)
		if err != nil {
			g.reject(w, r, http.StatusUnauthorized, ReasonUnauthorized, "unauthorized: "+err.Error())

			return
		}

		// Tokens minted before peer names were constrained (or by
		// another issuer) can carry a name no hostname strategy could
		// route. Refuse it here so a peer is never attached under a
		// name the proxy cannot address; re-enroll fixes it.
		if nameErr := peername.Validate(peer); nameErr != nil {
			g.reject(w, r, http.StatusForbidden, ReasonInvalidName, "forbidden: "+nameErr.Error())

			return
		}

		if g.Blocked != nil && g.Blocked.IsBlocked(peer) {
			g.reject(w, r, http.StatusForbidden, ReasonBlocked, "forbidden: peer is blocked")

			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), peerKey{}, peer)))
	})
}

// reject counts the refusal and answers the attach.
func (g Guard) reject(w http.ResponseWriter, r *http.Request, status int, reason, msg string) {
	if g.OnReject != nil {
		g.OnReject(r.Context(), reason)
	}

	http.Error(w, msg, status)
}

// peerKey is the context key the guard stamps the verified peer under.
type peerKey struct{}

// ErrUnauthenticated is returned by PeerFrom for a context the guard
// never stamped, which can only happen if the handler is mounted
// without it.
var ErrUnauthenticated = errors.New("unauthenticated")

// PeerFrom returns the peer id the guard verified for this request. Its
// signature is attach.Identity, so it wires straight into attach.NewHandler.
func PeerFrom(ctx context.Context) (string, error) {
	peer, _ := ctx.Value(peerKey{}).(string)
	if peer == "" {
		return "", ErrUnauthenticated
	}

	return peer, nil
}
