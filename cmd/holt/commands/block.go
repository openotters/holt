package commands

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	c "github.com/merlindorin/go-shared/pkg/cmd"

	holtv1 "github.com/openotters/holt/api/v1"
	"github.com/openotters/holt/cmd/holt/internal/style"
)

// Block stops a peer's tunnel AND bans its peer id (the JWT subject),
// so it cannot reconnect even with a valid token — versus kill's plain
// disconnect. The ban is on the identity, not one specific token:
// every token for that id, including freshly enrolled ones, is refused
// until `holt unblock`. The block is durable (SQLite).
type Block struct {
	Peer string `arg:"" help:"Peer id to ban (the JWT subject)."`
	adminConn
}

func (b *Block) Run(ctx context.Context, _ *c.Commons) error {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	client, err := b.client()
	if err != nil {
		return err
	}

	resp, err := client.BlockPeer(reqCtx,
		connect.NewRequest(&holtv1.BlockPeerRequest{Peer: b.Peer}))
	if err != nil {
		return fmt.Errorf("reaching hub: %w", err)
	}

	if resp.Msg.GetStopped() {
		fmt.Println(style.Success("blocked %q and closed its tunnel", b.Peer))
	} else {
		fmt.Println(style.Success("blocked %q (no live tunnel to close)", b.Peer))
	}

	fmt.Println(style.Note("the ban is on the peer id: every token for %q, even a freshly", b.Peer))
	fmt.Println(style.Note("enrolled one, is refused until you run `holt unblock %s`", b.Peer))

	return nil
}

// Unblock lifts a peer's ban so it may reconnect. Note that unblocking
// re-admits any token minted before the block that has not expired
// yet; blocking never invalidates the tokens themselves.
type Unblock struct {
	Peer string `arg:"" help:"Peer id whose ban to lift."`
	adminConn
}

func (u *Unblock) Run(ctx context.Context, _ *c.Commons) error {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	client, err := u.client()
	if err != nil {
		return err
	}

	if _, unblockErr := client.UnblockPeer(reqCtx,
		connect.NewRequest(&holtv1.UnblockPeerRequest{Peer: u.Peer})); unblockErr != nil {
		return fmt.Errorf("reaching hub: %w", unblockErr)
	}

	fmt.Println(style.Success("unblocked %q", u.Peer))
	fmt.Println(style.Note("%q can attach again; tokens minted before the block still work", u.Peer))
	fmt.Println(style.Note("until they expire (mint a fresh one with `holt enroll %s`)", u.Peer))

	return nil
}
