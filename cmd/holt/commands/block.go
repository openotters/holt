package commands

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	c "github.com/merlindorin/go-shared/pkg/cmd"

	holtv1 "github.com/openotters/holt/api/v1"
)

// Block stops a peer's tunnel AND blocks its credential, so it cannot
// reconnect even with a valid token — a hard revoke, versus kill's
// plain disconnect. The block is durable (SQLite) and lifted with
// `holt unblock`.
type Block struct {
	Peer      string `arg:"" help:"Peer whose credential to block."`
	AdminAddr string `help:"Hub admin address." default:"127.0.0.1:7001"`
}

// Run blocks the peer via the Admin service.
func (b *Block) Run(ctx context.Context, _ *c.Commons) error {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := adminClient(b.AdminAddr).BlockPeer(reqCtx,
		connect.NewRequest(&holtv1.BlockPeerRequest{Peer: b.Peer}))
	if err != nil {
		return fmt.Errorf("reaching hub at %s: %w", b.AdminAddr, err)
	}

	if resp.Msg.GetStopped() {
		fmt.Printf("blocked %q and closed its tunnel\n", b.Peer)
	} else {
		fmt.Printf("blocked %q (no live tunnel to close)\n", b.Peer)
	}

	return nil
}

// Unblock lifts a peer's block so it may reconnect.
type Unblock struct {
	Peer      string `arg:"" help:"Peer whose block to lift."`
	AdminAddr string `help:"Hub admin address." default:"127.0.0.1:7001"`
}

// Run unblocks the peer via the Admin service.
func (u *Unblock) Run(ctx context.Context, _ *c.Commons) error {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if _, err := adminClient(u.AdminAddr).UnblockPeer(reqCtx,
		connect.NewRequest(&holtv1.UnblockPeerRequest{Peer: u.Peer})); err != nil {
		return fmt.Errorf("reaching hub at %s: %w", u.AdminAddr, err)
	}

	fmt.Printf("unblocked %q\n", u.Peer)

	return nil
}
