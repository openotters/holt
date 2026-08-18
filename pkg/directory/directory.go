// Package directory defines peer presence: which peer is attached to
// which hub, since when. The in-memory implementation covers a single
// hub; the sqldir subpackages share presence across a fleet.
package directory

import (
	"context"
	"sync"
	"time"
)

// PeerRecord is one peer's presence entry: the shareable projection of
// a live tunnel, everything except the connection itself, which only
// the owning hub process holds.
type PeerRecord struct {
	Peer        string
	Hub         string // hub instance that owns the live tunnel
	PeerVersion string
	AttachedAt  time.Time
}

// Directory is the pluggable presence backend behind a Registry. It
// answers "is peer X attached, and to which hub?" and never holds live
// connections. The Registry calls it best-effort: a Directory error is
// logged but never fails a live attach or detach.
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
	// ClearHub removes every record owned by hub — called on boot with
	// the hub's own stable id to clear rows left behind by a crash.
	ClearHub(ctx context.Context, hub string) error
}

// MemoryDirectory is the default Directory: a mutex-guarded map,
// correct for a single hub. Safe for concurrent use.
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

func sortRecords(recs []PeerRecord) {
	for i := 1; i < len(recs); i++ {
		for j := i; j > 0 && recs[j-1].Peer > recs[j].Peer; j-- {
			recs[j-1], recs[j] = recs[j], recs[j-1]
		}
	}
}

var _ Directory = (*MemoryDirectory)(nil)
