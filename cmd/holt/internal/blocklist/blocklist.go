// Package blocklist is the hub's identity denylist: the peer ids
// refused at attach time, durable across restarts.
//
// Blocking bans an id, not one token: any token with that subject is
// refused while the entry exists, so a leaked token is stopped without
// rotating the signing secret on everyone else.
package blocklist

import (
	"sort"
	"sync"
	"time"

	"github.com/openotters/holt/cmd/holt/internal/store"
	"github.com/openotters/holt/pkg/admin"
)

// List is the denylist, keyed by JWT subject (peer id) → the unix time
// it was blocked. It is durable — backed by the SQLite store — with an
// in-memory cache for the hot IsBlocked path the attach guard hits on
// every connection. It satisfies both admin.Blocker (the Admin RPCs)
// and jwtauth.Blocker (the attach guard).
type List struct {
	st *store.Store

	mu      sync.RWMutex
	blocked map[string]int64
}

// New loads the persisted blocklist from the store.
func New(st *store.Store) (*List, error) {
	blocked, err := st.LoadBlocked()
	if err != nil {
		return nil, err
	}

	return &List{st: st, blocked: blocked}, nil
}

// Block denies future attaches for peer and persists the block.
func (l *List) Block(peer string) {
	at := time.Now().Unix()

	l.mu.Lock()
	l.blocked[peer] = at
	l.mu.Unlock()

	_ = l.st.Block(peer, at)
}

// Unblock lifts a block and persists the removal.
func (l *List) Unblock(peer string) {
	l.mu.Lock()
	delete(l.blocked, peer)
	l.mu.Unlock()

	_ = l.st.Unblock(peer)
}

// IsBlocked reports whether peer is currently blocked (cache read).
func (l *List) IsBlocked(peer string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()

	_, ok := l.blocked[peer]

	return ok
}

// Blocked returns the blocked peers, ordered by peer id.
func (l *List) Blocked() []admin.BlockedPeer {
	l.mu.RLock()
	out := make([]admin.BlockedPeer, 0, len(l.blocked))

	for peer, at := range l.blocked {
		out = append(out, admin.BlockedPeer{Peer: peer, BlockedAtUnix: at})
	}
	l.mu.RUnlock()

	sort.Slice(out, func(i, j int) bool { return out[i].Peer < out[j].Peer })

	return out
}
