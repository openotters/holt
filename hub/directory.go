package hub

import (
	"context"
	"sync"
	"time"
)

// PeerRecord is one peer's presence entry: which hub currently owns
// its live tunnel, its build version, and when it attached. This is
// the durable, shareable projection of a live tunnel — everything
// EXCEPT the tunnel itself, which is a live *http2.ClientConn that
// only its owning hub process holds.
type PeerRecord struct {
	Peer        string
	Hub         string // hub instance that owns the live tunnel
	PeerVersion string
	AttachedAt  time.Time
}

// Directory is the pluggable presence backend behind a Registry. It
// records attach/detach and answers presence queries. Implementations
// range from in-memory (single hub, default) to SQL (sqlite/postgres,
// shared across a fleet so any hub can see who is attached where).
//
// A Directory never holds live connections — RoundTripper always
// resolves against the owning hub's local session map. The Directory
// is for routing and observability: "is peer X attached, and to which
// hub?" Cross-hub request forwarding, if wanted, is the application's
// job on top of Lookup.
//
// All methods take a context and may do I/O. The Registry calls them
// best-effort — a Directory error is logged but never fails a live
// attach or detach, so a database blip cannot sever a working tunnel.
type Directory interface {
	// Attach records (or upserts) a peer as attached to rec.Hub.
	Attach(ctx context.Context, rec PeerRecord) error
	// Detach removes peer's record, but only if hub still owns it —
	// a stale detach from a superseded owner must not evict the new
	// one.
	Detach(ctx context.Context, peer, hub string) error
	// Lookup returns peer's record and whether it was found.
	Lookup(ctx context.Context, peer string) (PeerRecord, bool, error)
	// List returns every attached peer, ordered by peer id.
	List(ctx context.Context) ([]PeerRecord, error)
	// ClearHub removes every record owned by hub — a hub calls this on
	// boot with its own stable id to clear rows it left behind after a
	// crash.
	ClearHub(ctx context.Context, hub string) error
}

// MemoryDirectory is the default Directory: a mutex-guarded map. Zero
// dependencies, correct for a single hub. Safe for concurrent use.
type MemoryDirectory struct {
	mu   sync.Mutex
	recs map[string]PeerRecord
}

// NewMemoryDirectory returns an empty in-memory directory.
func NewMemoryDirectory() *MemoryDirectory {
	return &MemoryDirectory{recs: make(map[string]PeerRecord)}
}

func (d *MemoryDirectory) Attach(_ context.Context, rec PeerRecord) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.recs[rec.Peer] = rec

	return nil
}

func (d *MemoryDirectory) Detach(_ context.Context, peer, hub string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if rec, ok := d.recs[peer]; ok && rec.Hub == hub {
		delete(d.recs, peer)
	}

	return nil
}

func (d *MemoryDirectory) Lookup(_ context.Context, peer string) (PeerRecord, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	rec, ok := d.recs[peer]

	return rec, ok, nil
}

func (d *MemoryDirectory) List(_ context.Context) ([]PeerRecord, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	out := make([]PeerRecord, 0, len(d.recs))
	for _, rec := range d.recs {
		out = append(out, rec)
	}

	sortRecords(out)

	return out, nil
}

func (d *MemoryDirectory) ClearHub(_ context.Context, hub string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	for peer, rec := range d.recs {
		if rec.Hub == hub {
			delete(d.recs, peer)
		}
	}

	return nil
}

// sortRecords orders records by peer id for stable List output.
func sortRecords(recs []PeerRecord) {
	for i := 1; i < len(recs); i++ {
		for j := i; j > 0 && recs[j-1].Peer > recs[j].Peer; j-- {
			recs[j-1], recs[j] = recs[j], recs[j-1]
		}
	}
}

var _ Directory = (*MemoryDirectory)(nil)
