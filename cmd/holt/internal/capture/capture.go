// Package capture runs short-lived capture endpoints inside the hub
// process. Each endpoint is an ordinary peer — enrolled with the hub's
// own secret, attached through the real tunnel listener over loopback —
// whose handler acknowledges whatever arrives. The proxy, the roster,
// and the traffic view treat it like any peer; nothing in the data
// plane is special-cased.
package capture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/openotters/holt/pkg/dial"
	"github.com/openotters/holt/pkg/jwtauth"
	"github.com/openotters/holt/pkg/peername"
)

// DefaultTTL applies when the caller passes no TTL; MaxTTL is the
// ceiling. Anything longer-lived should be a real peer.
const (
	DefaultTTL = time.Hour
	MaxTTL     = 24 * time.Hour
)

// Bin is one live capture endpoint. The JSON names are the console's
// contract.
type Bin struct {
	Peer      string    `json:"peer"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// Tunnels is the registry surface the manager needs; *registry.Registry
// satisfies it.
type Tunnels interface {
	Attached(peer string) bool
	StopTunnel(peer, reason string) bool
}

// Manager owns the hub's capture endpoints. Safe for concurrent use.
type Manager struct {
	// ctx is the hub run lifecycle; endpoint contexts derive from it,
	// so no endpoint outlives the hub.
	ctx       context.Context
	tunnelURL string
	secret    *jwtauth.Secret
	tunnels   Tunnels
	logger    *zap.Logger

	mu   sync.Mutex
	bins map[string]*bin
}

type bin struct {
	info   Bin
	cancel context.CancelFunc
}

// NewManager builds a manager dialing tunnelURL — the hub's own tunnel
// listener — with tokens signed by secret.
func NewManager(
	ctx context.Context, tunnelURL string, secret *jwtauth.Secret, tunnels Tunnels, logger *zap.Logger,
) *Manager {
	return &Manager{
		ctx:       ctx,
		tunnelURL: tunnelURL,
		secret:    secret,
		tunnels:   tunnels,
		logger:    logger.Named("holt-capture"),
		bins:      make(map[string]*bin),
	}
}

// Create starts a capture endpoint. An empty name gets a generated one
// (built like any peer's, see peername.Random); a zero ttl means
// DefaultTTL, anything above MaxTTL is clamped. Attaching itself is
// asynchronous, like any peer's.
func (m *Manager) Create(name string, ttl time.Duration) (Bin, error) {
	if ttl <= 0 {
		ttl = DefaultTTL
	}

	ttl = min(ttl, MaxTTL)

	if name == "" {
		generated, err := peername.Random()
		if err != nil {
			return Bin{}, err
		}

		name = generated
	} else if err := peername.Validate(name); err != nil {
		return Bin{}, err
	}

	now := time.Now()
	info := Bin{Peer: name, CreatedAt: now, ExpiresAt: now.Add(ttl)}

	// The token lives exactly as long as the endpoint: redials inside
	// the window work, nothing usable is left after it.
	token, err := jwtauth.Issue(m.secret.Get(), name, m.tunnelURL, ttl)
	if err != nil {
		return Bin{}, err
	}

	m.mu.Lock()

	if _, taken := m.bins[name]; taken {
		m.mu.Unlock()

		return Bin{}, fmt.Errorf("capture endpoint %q already exists", name)
	}

	// Attaching under a live peer's name would supersede its tunnel.
	if m.tunnels.Attached(name) {
		m.mu.Unlock()

		return Bin{}, fmt.Errorf("a peer named %q is attached; pick another name", name)
	}

	//nolint:gosec // G118: cancel is kept on the bin — run calls it when the loop ends, Stop calls it early.
	binCtx, cancel := context.WithDeadline(m.ctx, info.ExpiresAt)
	b := &bin{info: info, cancel: cancel}
	m.bins[name] = b

	m.mu.Unlock()

	go m.run(binCtx, b, token)

	return info, nil
}

// run is one endpoint's attach loop, ended by the TTL deadline, Stop,
// an operator kill, or hub shutdown.
func (m *Manager) run(ctx context.Context, b *bin, token string) {
	err := dial.Run(ctx, dial.Options{
		URL:     m.tunnelURL,
		Header:  http.Header{"Authorization": {"Bearer " + token}},
		Handler: acknowledge(b.info.Peer),
		Version: "capture",
		Logger:  m.logger,
		// Loopback with nothing between: WebSocket keepalives add
		// nothing (the inner HTTP/2 PING still guards liveness).
		Keepalive: -1,
	})

	m.mu.Lock()
	if m.bins[b.info.Peer] == b {
		delete(m.bins, b.info.Peer)
	}
	m.mu.Unlock()

	b.cancel()

	// A non-nil error means our own lifecycle ended the loop; without
	// this the hub notices the vanished peer only after its ping
	// window. Nil means the hub already detached it (kill, supersede) —
	// the name may have a new owner, so it is not ours to stop.
	if err != nil {
		reason := "capture-stopped"
		if errors.Is(err, context.DeadlineExceeded) {
			reason = "capture-expired"
		}

		m.tunnels.StopTunnel(b.info.Peer, reason)
	}

	m.logger.Debug("capture endpoint ended",
		zap.String("peer", b.info.Peer), zap.Error(err))
}

// List returns the live endpoints, oldest first.
func (m *Manager) List() []Bin {
	m.mu.Lock()

	out := make([]Bin, 0, len(m.bins))
	for _, b := range m.bins {
		out = append(out, b.info)
	}

	m.mu.Unlock()

	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}

		return out[i].Peer < out[j].Peer
	})

	return out
}

// Stop ends the named endpoint now and reports whether it existed.
func (m *Manager) Stop(name string) bool {
	m.mu.Lock()
	b := m.bins[name]
	delete(m.bins, name)
	m.mu.Unlock()

	if b == nil {
		return false
	}

	b.cancel()

	return true
}

// acknowledge answers 200 with a small JSON receipt, draining the body
// so the proxy's capture sees it in full.
func acknowledge(peer string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		_ = json.NewEncoder(w).Encode(map[string]any{
			"captured": true,
			"capture":  peer,
			"method":   r.Method,
			"path":     r.URL.RequestURI(),
		})
	})
}
