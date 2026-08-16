// Package registry keeps the hub's live tunnels keyed by peer ID and
// lets the application dial "through" any attached peer with an
// ordinary http.RoundTripper. Attach/detach events double as the
// peers' presence signal.
//
// Presence can be projected to a pluggable directory.Directory
// (in-memory by default; SQL for a shared fleet — see
// internal/directory/sqldir). The live tunnels themselves are always
// local to the owning hub process; the directory only records who is
// attached where.
package registry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
	"golang.org/x/net/http2"

	"github.com/openotters/holt/internal/wire"
	"github.com/openotters/holt/pkg/directory"
)

// ErrPeerDetached is returned by the per-peer RoundTripper when no
// tunnel is attached. Applications use errors.Is to map it onto
// their own "not reachable" error shape.
var ErrPeerDetached = errors.New("holt: peer not attached")

// dirTimeout bounds each best-effort Directory call so a slow store
// cannot stall attach/detach.
const dirTimeout = 5 * time.Second

// EventKind labels a Registry event.
type EventKind uint8

const (
	EventAttached EventKind = iota + 1
	EventDetached
)

// Event is one attach/detach transition. Reason is set on detach
// only ("superseded", "connection-lost", application reasons, …).
type Event struct {
	Peer   string
	Kind   EventKind
	Reason string
	At     time.Time
}

// TunnelInfo describes one live tunnel this hub owns. It is the local,
// live view — distinct from a Directory PeerRecord, which is the
// durable projection that may span hubs.
type TunnelInfo struct {
	Peer        string
	PeerVersion string
	AttachedAt  time.Time
}

// entry is one live tunnel: the hub-side HTTP/2 session, the close
// hook the Attach handler registered, and light metadata for the
// operational surface.
type entry struct {
	cc         *http2.ClientConn
	close      func(reason string)
	version    string
	attachedAt time.Time
}

// watchChanSize buffers each Watch subscriber. Sends are
// non-blocking: a slow subscriber misses events rather than stalling
// attach/detach; consumers re-check live state (Attached) when it
// matters, so a dropped event degrades latency, not correctness.
const watchChanSize = 64

// Registry tracks the live tunnel per peer. Safe for concurrent use.
type Registry struct {
	logger  *zap.Logger
	hubID   string
	dir     directory.Directory
	metrics *metrics

	mu      sync.Mutex
	conns   map[string]*entry
	subs    map[uint64]chan Event
	nextSub uint64
}

// Option configures a Registry.
type Option func(*Registry)

// WithHubID sets this hub's stable instance id, recorded in the
// Directory so a fleet can tell which hub owns a peer. Give each hub a
// stable, unique id (hostname, pod name) in a multi-hub deployment so
// a restarting hub can clear its own stale rows. Default "local".
func WithHubID(id string) Option {
	return func(r *Registry) { r.hubID = id }
}

// WithDirectory sets the presence backend. Default is an in-memory
// directory (correct for a single hub); pass a SQL directory to share
// presence across a fleet.
func WithDirectory(dir directory.Directory) Option {
	return func(r *Registry) { r.dir = dir }
}

// WithMeterProvider sets the OTel MeterProvider for tunnel metrics.
// Optional — without it the global provider is used, which is a no-op
// until the application installs an SDK.
func WithMeterProvider(mp metric.MeterProvider) Option {
	return func(r *Registry) { r.metrics = newMetrics(mp, func() int64 { return int64(r.CountTunnels()) }) }
}

// NewRegistry builds a Registry. By default it keeps presence
// in-memory, identifies itself as "local", and records metrics
// against the global (no-op) OTel provider.
func NewRegistry(logger *zap.Logger, opts ...Option) *Registry {
	r := &Registry{
		logger: logger.Named("holt-hub"),
		hubID:  "local",
		dir:    directory.NewMemoryDirectory(),
		conns:  make(map[string]*entry),
		subs:   make(map[uint64]chan Event),
	}
	for _, opt := range opts {
		opt(r)
	}

	if r.metrics == nil {
		r.metrics = newMetrics(nil, func() int64 { return int64(r.CountTunnels()) })
	}

	return r
}

// HubID returns this hub's instance id.
func (r *Registry) HubID() string { return r.hubID }

// ClearStale removes any Directory rows this hub left behind after a
// crash. Call once on boot, before accepting attaches.
func (r *Registry) ClearStale(ctx context.Context) error {
	return r.dir.ClearHub(ctx, r.hubID)
}

// Attach registers a live tunnel for peer. version is the peer's
// self-reported build (from the Hello frame). If a tunnel already
// exists it is closed with "superseded" and replaced — a
// crashed-and-redialed peer must never wait out a keepalive timeout
// on its own corpse. Returns a detach func the Attach handler calls
// with the detach reason; detach is idempotent and only removes THIS
// entry (a superseding attach can't be clobbered by the loser's
// cleanup).
func (r *Registry) Attach(
	peer, version string, cc *http2.ClientConn, closeTunnel func(reason string),
) func(reason string) {
	now := time.Now()
	e := &entry{cc: cc, close: closeTunnel, version: version, attachedAt: now}

	r.mu.Lock()
	old := r.conns[peer]
	r.conns[peer] = e
	r.broadcastLocked(Event{Peer: peer, Kind: EventAttached, At: now})
	r.mu.Unlock()

	if old != nil {
		old.close("superseded")
	}

	r.metrics.recordAttach(context.Background())
	r.dirAttach(directory.PeerRecord{Peer: peer, Hub: r.hubID, PeerVersion: version, AttachedAt: now})

	var once sync.Once

	return func(reason string) {
		once.Do(func() {
			r.mu.Lock()
			owned := r.conns[peer] == e
			if owned {
				delete(r.conns, peer)
				r.broadcastLocked(Event{
					Peer: peer, Kind: EventDetached, Reason: reason, At: time.Now(),
				})
			}
			r.mu.Unlock()

			if !owned {
				return // superseded — the newer tunnel owns the slot
			}

			r.metrics.recordDetach(context.Background(), reason)
			r.dirDetach(peer)
		})
	}
}

// Attached reports whether peer currently has a live tunnel ON THIS
// hub. For a fleet-wide answer, use LookupPeer.
func (r *Registry) Attached(peer string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, ok := r.conns[peer]

	return ok
}

// Tunnel returns the live tunnel info for peer on this hub.
func (r *Registry) Tunnel(peer string) (TunnelInfo, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	e, ok := r.conns[peer]
	if !ok {
		return TunnelInfo{}, false
	}

	return TunnelInfo{Peer: peer, PeerVersion: e.version, AttachedAt: e.attachedAt}, true
}

// ListTunnels returns every live tunnel this hub owns, ordered by
// peer id.
func (r *Registry) ListTunnels() []TunnelInfo {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]TunnelInfo, 0, len(r.conns))
	for peer, e := range r.conns {
		out = append(out, TunnelInfo{Peer: peer, PeerVersion: e.version, AttachedAt: e.attachedAt})
	}

	// Small n; insertion sort keeps output stable without importing sort.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].Peer > out[j].Peer; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}

	return out
}

// CountTunnels returns the number of live tunnels on this hub.
func (r *Registry) CountTunnels() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.conns)
}

// StopTunnel force-closes peer's tunnel with reason and reports
// whether a tunnel was present. The Attach handler's detach emits the
// event and updates the Directory.
func (r *Registry) StopTunnel(peer, reason string) bool {
	r.mu.Lock()
	e := r.conns[peer]
	r.mu.Unlock()

	if e == nil {
		return false
	}

	e.close(reason)

	return true
}

// StopAllTunnels closes every live tunnel — hub shutdown/drain.
func (r *Registry) StopAllTunnels(reason string) {
	r.mu.Lock()
	entries := make([]*entry, 0, len(r.conns))
	for _, e := range r.conns {
		entries = append(entries, e)
	}
	r.mu.Unlock()

	for _, e := range entries {
		e.close(reason)
	}
}

// LookupPeer returns the Directory record for peer — a fleet-wide
// answer to "is this peer attached, and to which hub?".
func (r *Registry) LookupPeer(ctx context.Context, peer string) (directory.PeerRecord, bool, error) {
	return r.dir.Lookup(ctx, peer)
}

// Peers returns every attached peer known to the Directory (fleet-wide
// when the Directory is shared).
func (r *Registry) Peers(ctx context.Context) ([]directory.PeerRecord, error) {
	return r.dir.List(ctx)
}

// Watch streams attach/detach events until ctx ends. The channel is
// buffered (watchChanSize) and lossy for slow consumers.
func (r *Registry) Watch(ctx context.Context) <-chan Event {
	ch := make(chan Event, watchChanSize)

	r.mu.Lock()
	id := r.nextSub
	r.nextSub++
	r.subs[id] = ch
	r.mu.Unlock()

	go func() {
		<-ctx.Done()

		r.mu.Lock()
		defer r.mu.Unlock()

		delete(r.subs, id)
		close(ch)
	}()

	return ch
}

// broadcastLocked sends ev to every subscriber without blocking;
// callers hold r.mu, which is what makes the close in Watch's
// goroutine race-free against sends.
func (r *Registry) broadcastLocked(ev Event) {
	for _, ch := range r.subs {
		select {
		case ch <- ev:
		default:
			r.logger.Warn("holt: dropping event for slow watcher",
				zap.String("peer", ev.Peer))
		}
	}
}

// dirAttach records presence best-effort — a Directory error is
// logged but never fails a live attach.
func (r *Registry) dirAttach(rec directory.PeerRecord) {
	ctx, cancel := context.WithTimeout(context.Background(), dirTimeout)
	defer cancel()

	if err := r.dir.Attach(ctx, rec); err != nil {
		r.logger.Warn("holt: directory attach failed",
			zap.String("peer", rec.Peer), zap.Error(err))
	}
}

func (r *Registry) dirDetach(peer string) {
	ctx, cancel := context.WithTimeout(context.Background(), dirTimeout)
	defer cancel()

	if err := r.dir.Detach(ctx, peer, r.hubID); err != nil {
		r.logger.Warn("holt: directory detach failed",
			zap.String("peer", peer), zap.Error(err))
	}
}

// RoundTripper returns a stable http.RoundTripper for peer. Each
// RoundTrip resolves the CURRENT session, so a reattach mid-lifetime
// is transparent to callers holding the RoundTripper. Requests fail
// with ErrPeerDetached (use errors.Is) when no tunnel is attached ON
// THIS hub — RoundTripper never crosses hubs, since only the owning
// hub holds the live connection.
func (r *Registry) RoundTripper(peer string) http.RoundTripper {
	return roundTripper{registry: r, peer: peer}
}

type roundTripper struct {
	registry *Registry
	peer     string
}

func (rt roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.registry.mu.Lock()
	e := rt.registry.conns[rt.peer]
	rt.registry.mu.Unlock()

	if e == nil {
		return nil, fmt.Errorf("peer %s: %w", rt.peer, ErrPeerDetached)
	}

	return e.cc.RoundTrip(req)
}

// Well-known detach reasons, as carried by the tunnel's GoAway frame.
// Superseded, credential revocation, and deliberate stop are terminal
// for the peer (no redial); everything else means "redial with
// backoff".
const (
	ReasonSuperseded   = wire.ReasonSuperseded
	ReasonTokenRevoked = wire.ReasonTokenRevoked
	ReasonShuttingDown = wire.ReasonShuttingDown
	ReasonPeerStopping = wire.ReasonPeerStopping
	ReasonClosed       = wire.ReasonClosed
)
