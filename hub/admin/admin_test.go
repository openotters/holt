package admin_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"go.uber.org/zap"

	holtv1 "github.com/openotters/holt/api/v1"
	"github.com/openotters/holt/api/v1/holtv1connect"
	"github.com/openotters/holt/hub"
	"github.com/openotters/holt/hub/admin"
)

func TestAdminService(t *testing.T) {
	t.Parallel()

	reg := hub.NewRegistry(zap.NewNop())
	svc := admin.NewService(reg)
	ctx := context.Background()

	// Two live tunnels. Wire each close hook to its detach so
	// StopTunnel actually removes the entry — as the real handler does
	// when the session closes.
	var detachAlice, detachBob func(string)
	detachAlice = reg.Attach("alice", "v1", nil, func(r string) { detachAlice(r) })
	detachBob = reg.Attach("bob", "v2", nil, func(r string) { detachBob(r) })
	_ = detachAlice

	resp, err := svc.ListTunnels(ctx, connect.NewRequest(&holtv1.ListTunnelsRequest{}))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if got := len(resp.Msg.GetTunnels()); got != 2 {
		t.Fatalf("listed %d tunnels, want 2", got)
	}

	stop, err := svc.StopTunnel(ctx, connect.NewRequest(&holtv1.StopTunnelRequest{Peer: "bob"}))
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if !stop.Msg.GetStopped() {
		t.Fatal("StopTunnel reported no tunnel for bob")
	}

	resp, _ = svc.ListTunnels(ctx, connect.NewRequest(&holtv1.ListTunnelsRequest{}))
	if got := len(resp.Msg.GetTunnels()); got != 1 || resp.Msg.GetTunnels()[0].GetPeer() != "alice" {
		t.Fatalf("after stop: %+v", resp.Msg.GetTunnels())
	}

	// Stopping an unknown peer reports false.
	stop, _ = svc.StopTunnel(ctx, connect.NewRequest(&holtv1.StopTunnelRequest{Peer: "nobody"}))
	if stop.Msg.GetStopped() {
		t.Fatal("StopTunnel reported stopped for an unknown peer")
	}
}

// fakeBlocker is an in-memory admin.Blocker for the block-path test.
type fakeBlocker struct {
	blocked map[string]bool
}

func (f *fakeBlocker) Block(peer string)   { f.blocked[peer] = true }
func (f *fakeBlocker) Unblock(peer string) { delete(f.blocked, peer) }

func (f *fakeBlocker) Blocked() []admin.BlockedPeer {
	out := make([]admin.BlockedPeer, 0, len(f.blocked))
	for peer := range f.blocked {
		out = append(out, admin.BlockedPeer{Peer: peer, BlockedAtUnix: 1})
	}

	return out
}

func TestAdminService_Block(t *testing.T) {
	t.Parallel()

	reg := hub.NewRegistry(zap.NewNop())
	blocker := &fakeBlocker{blocked: map[string]bool{}}
	svc := admin.NewService(reg, admin.WithBlocker(blocker))
	ctx := context.Background()

	var detach func(string)
	detach = reg.Attach("alice", "v1", nil, func(r string) { detach(r) })

	// BlockPeer closes the tunnel and records the block.
	resp, err := svc.BlockPeer(ctx, connect.NewRequest(&holtv1.BlockPeerRequest{Peer: "alice"}))
	if err != nil || !resp.Msg.GetStopped() {
		t.Fatalf("BlockPeer: %v stopped=%v", err, resp.Msg.GetStopped())
	}

	blocked, err := svc.ListBlocked(ctx, connect.NewRequest(&holtv1.ListBlockedRequest{}))
	if err != nil {
		t.Fatalf("ListBlocked: %v", err)
	}
	if len(blocked.Msg.GetPeers()) != 1 || blocked.Msg.GetPeers()[0].GetPeer() != "alice" {
		t.Fatalf("ListBlocked = %+v, want [alice]", blocked.Msg.GetPeers())
	}

	// Unblock clears it.
	if _, err := svc.UnblockPeer(ctx, connect.NewRequest(&holtv1.UnblockPeerRequest{Peer: "alice"})); err != nil {
		t.Fatalf("UnblockPeer: %v", err)
	}
	blocked, _ = svc.ListBlocked(ctx, connect.NewRequest(&holtv1.ListBlockedRequest{}))
	if len(blocked.Msg.GetPeers()) != 0 {
		t.Fatalf("after unblock: %+v", blocked.Msg.GetPeers())
	}
}

func TestAdminService_BlockUnimplementedWithoutBlocker(t *testing.T) {
	t.Parallel()

	svc := admin.NewService(hub.NewRegistry(zap.NewNop()))

	_, err := svc.BlockPeer(context.Background(), connect.NewRequest(&holtv1.BlockPeerRequest{Peer: "x"}))
	if connect.CodeOf(err) != connect.CodeUnimplemented {
		t.Fatalf("BlockPeer without blocker = %v, want Unimplemented", err)
	}

	// ListBlocked without a blocker is empty, not an error.
	resp, err := svc.ListBlocked(context.Background(), connect.NewRequest(&holtv1.ListBlockedRequest{}))
	if err != nil || len(resp.Msg.GetPeers()) != 0 {
		t.Fatalf("ListBlocked without blocker = %+v, %v", resp.Msg.GetPeers(), err)
	}
}

// TestWatchTunnels exercises the stream over a real HTTP round-trip:
// snapshot first, then live attach/detach events.
func TestWatchTunnels(t *testing.T) {
	t.Parallel()

	reg := hub.NewRegistry(zap.NewNop())
	path, handler := holtv1connect.NewAdminHandler(admin.NewService(reg))
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// One tunnel up before subscribing: it must arrive as the snapshot.
	var detachAlice func(string)
	detachAlice = reg.Attach("alice", "v1", nil, func(r string) { detachAlice(r) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := holtv1connect.NewAdminClient(srv.Client(), srv.URL)
	stream, err := client.WatchTunnels(ctx, connect.NewRequest(&holtv1.WatchTunnelsRequest{}))
	if err != nil {
		t.Fatalf("watch: %v", err)
	}

	next := func() *holtv1.TunnelEvent {
		t.Helper()
		if !stream.Receive() {
			t.Fatalf("stream ended early: %v", stream.Err())
		}

		return stream.Msg()
	}

	if ev := next(); ev.GetKind() != holtv1.TunnelEvent_KIND_UNSPECIFIED {
		t.Fatalf("hello: got %v, want KIND_UNSPECIFIED", ev.GetKind())
	}

	if ev := next(); ev.GetKind() != holtv1.TunnelEvent_KIND_ATTACHED || ev.GetInfo().GetPeer() != "alice" {
		t.Fatalf("snapshot: got %v %q", ev.GetKind(), ev.GetInfo().GetPeer())
	}

	// Live attach.
	var detachBob func(string)
	detachBob = reg.Attach("bob", "v2", nil, func(r string) { detachBob(r) })
	_ = detachBob

	if ev := next(); ev.GetKind() != holtv1.TunnelEvent_KIND_ATTACHED || ev.GetInfo().GetPeer() != "bob" {
		t.Fatalf("live attach: got %v %q", ev.GetKind(), ev.GetInfo().GetPeer())
	}

	// Live detach, with the reason forwarded.
	reg.StopTunnel("alice", "test-reason")

	if ev := next(); ev.GetKind() != holtv1.TunnelEvent_KIND_DETACHED ||
		ev.GetInfo().GetPeer() != "alice" || ev.GetReason() != "test-reason" {
		t.Fatalf("live detach: got %v %q reason=%q", ev.GetKind(), ev.GetInfo().GetPeer(), ev.GetReason())
	}

	// Cancelling the context ends the stream client-side.
	cancel()

	// Drain until the stream closes.
	for stream.Receive() {
		continue
	}
}

func TestInfo(t *testing.T) {
	t.Parallel()

	reg := hub.NewRegistry(zap.NewNop())
	var detach func(string)
	detach = reg.Attach("alice", "v1", nil, func(r string) { detach(r) })

	svc := admin.NewService(reg, admin.WithInfo(admin.HubInfo{
		Version:       "1.2.3",
		AdvertiseAddr: "10.0.0.5:7000",
		ProxyAddr:     "127.0.0.1:7002",
		RouteHeader:   "x-tunnel-peer",
	}))

	resp, err := svc.Info(context.Background(), connect.NewRequest(&holtv1.InfoRequest{}))
	if err != nil {
		t.Fatalf("info: %v", err)
	}

	msg := resp.Msg
	if msg.GetVersion() != "1.2.3" || msg.GetAdvertiseAddr() != "10.0.0.5:7000" {
		t.Fatalf("static fields not reported: %+v", msg)
	}

	if msg.GetTunnels() != 1 {
		t.Fatalf("tunnels = %d, want 1", msg.GetTunnels())
	}
}
