package commands

import (
	"sort"
	"sync"
	"time"

	"github.com/openotters/holt/cmd/holt/internal/store"
	"github.com/openotters/holt/hub/admin"
)

// blockList is the hub's credential denylist, keyed by JWT subject
// (peer id) → the unix time it was blocked. It is durable — backed by
// the SQLite store — with an in-memory cache for the hot IsBlocked
// path the JWT middleware hits on every attach. Implements
// admin.Blocker (Block / Unblock / Blocked).
type blockList struct {
	st *store.Store

	mu      sync.RWMutex
	blocked map[string]int64
}

// newBlockList loads the persisted blocklist from the store.
func newBlockList(st *store.Store) (*blockList, error) {
	blocked, err := st.LoadBlocked()
	if err != nil {
		return nil, err
	}

	return &blockList{st: st, blocked: blocked}, nil
}

// Block denies future attaches for peer and persists the block.
func (b *blockList) Block(peer string) {
	at := time.Now().Unix()

	b.mu.Lock()
	b.blocked[peer] = at
	b.mu.Unlock()

	_ = b.st.Block(peer, at)
}

// Unblock lifts a block and persists the removal.
func (b *blockList) Unblock(peer string) {
	b.mu.Lock()
	delete(b.blocked, peer)
	b.mu.Unlock()

	_ = b.st.Unblock(peer)
}

// IsBlocked reports whether peer is currently blocked (cache read).
func (b *blockList) IsBlocked(peer string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	_, ok := b.blocked[peer]

	return ok
}

// Blocked returns the blocked peers, ordered by peer id.
func (b *blockList) Blocked() []admin.BlockedPeer {
	b.mu.RLock()
	out := make([]admin.BlockedPeer, 0, len(b.blocked))
	for peer, at := range b.blocked {
		out = append(out, admin.BlockedPeer{Peer: peer, BlockedAtUnix: at})
	}
	b.mu.RUnlock()

	sort.Slice(out, func(i, j int) bool { return out[i].Peer < out[j].Peer })

	return out
}
