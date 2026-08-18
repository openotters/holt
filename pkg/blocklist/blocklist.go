// Package blocklist is the hub's identity denylist: the peer ids
// refused at attach time, durable across restarts. A block bans an id,
// not one token, so a leaked token is stopped without rotating the
// signing secret on everyone else. Storage is a pluggable Store
// (SQLite or PostgreSQL); reads go to the store, so a block issued by
// another hub on a shared store takes effect everywhere, and an
// in-memory snapshot answers when the store is unreachable.
package blocklist

import (
	"context"
	"sort"
	"sync"
	"time"
)

// Store persists the denylist. NewSQL provides the SQL implementation
// for both supported dialects.
type Store interface {
	// Load returns every blocked peer, keyed by peer id, valued by the
	// unix time it was blocked.
	Load(ctx context.Context) (map[string]int64, error)
	IsBlocked(ctx context.Context, peer string) (bool, error)
	// Block records a block (unix seconds), keeping the original time
	// if the peer is already blocked.
	Block(ctx context.Context, peer string, atUnix int64) error
	Unblock(ctx context.Context, peer string) error
}

// BlockedPeer is one entry in the denylist.
type BlockedPeer struct {
	Peer          string
	BlockedAtUnix int64
}

// storeTimeout bounds each store call so an unreachable backend
// degrades to the snapshot instead of stalling an attach.
const storeTimeout = 2 * time.Second

// List is the denylist over a pluggable Store. It satisfies both the
// admin service's Blocker (Block/Unblock/Blocked) and the attach
// guard's Blocker (IsBlocked).
type List struct {
	st Store

	mu    sync.RWMutex
	cache map[string]int64 // last known snapshot, the fallback
}

// New loads the persisted denylist from st.
func New(ctx context.Context, st Store) (*List, error) {
	blocked, err := st.Load(ctx)
	if err != nil {
		return nil, err
	}

	return &List{st: st, cache: blocked}, nil
}

// Block denies future attaches for peer and persists the block.
func (l *List) Block(peer string) {
	at := time.Now().Unix()

	l.mu.Lock()
	l.cache[peer] = at
	l.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()

	_ = l.st.Block(ctx, peer, at)
}

// Unblock lifts a block and persists the removal.
func (l *List) Unblock(peer string) {
	l.mu.Lock()
	delete(l.cache, peer)
	l.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()

	_ = l.st.Unblock(ctx, peer)
}

// IsBlocked reports whether peer is currently blocked. The store is
// asked first — on a shared store that makes another hub's block
// effective here — and the last known snapshot answers if the store
// errors.
func (l *List) IsBlocked(peer string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()

	blocked, err := l.st.IsBlocked(ctx, peer)
	if err != nil {
		l.mu.RLock()
		defer l.mu.RUnlock()

		_, ok := l.cache[peer]

		return ok
	}

	l.mu.Lock()
	if blocked {
		if _, ok := l.cache[peer]; !ok {
			l.cache[peer] = time.Now().Unix()
		}
	} else {
		delete(l.cache, peer)
	}
	l.mu.Unlock()

	return blocked
}

// Blocked returns the blocked peers, ordered by peer id. Served from
// the store — fleet-wide on a shared one — with the snapshot as the
// fallback.
func (l *List) Blocked() []BlockedPeer {
	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()

	blocked, err := l.st.Load(ctx)
	if err != nil {
		l.mu.RLock()
		blocked = make(map[string]int64, len(l.cache))
		for peer, at := range l.cache {
			blocked[peer] = at
		}
		l.mu.RUnlock()
	} else {
		l.mu.Lock()
		l.cache = blocked
		l.mu.Unlock()
	}

	out := make([]BlockedPeer, 0, len(blocked))
	for peer, at := range blocked {
		out = append(out, BlockedPeer{Peer: peer, BlockedAtUnix: at})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Peer < out[j].Peer })

	return out
}
