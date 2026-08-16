package registry_test

import (
	"context"
	"testing"

	"go.uber.org/zap"

	"github.com/openotters/holt/internal/directory"
	"github.com/openotters/holt/internal/registry"
)

// TestRegistry_DirectoryProjection confirms the Registry mirrors live
// attach/detach into its Directory, tagged with the hub id, so a
// fleet-wide LookupPeer/Peers reflects local tunnels.
func TestRegistry_DirectoryProjection(t *testing.T) {
	t.Parallel()

	dir := directory.NewMemoryDirectory()
	r := registry.NewRegistry(zap.NewNop(),
		registry.WithHubID("hub-1"),
		registry.WithDirectory(dir))

	ctx := context.Background()

	detach := r.Attach("peer-1", "v1", nil, func(string) {})

	rec, ok, err := r.LookupPeer(ctx, "peer-1")
	if err != nil || !ok {
		t.Fatalf("LookupPeer: %v ok=%v", err, ok)
	}
	if rec.Hub != "hub-1" || rec.PeerVersion != "v1" {
		t.Fatalf("record = %+v", rec)
	}

	peers, err := r.Peers(ctx)
	if err != nil || len(peers) != 1 || peers[0].Peer != "peer-1" {
		t.Fatalf("Peers = %+v, err=%v", peers, err)
	}

	detach("connection-lost")

	if _, ok, _ := r.LookupPeer(ctx, "peer-1"); ok {
		t.Fatal("directory still shows peer after detach")
	}
}

// TestRegistry_ClearStale confirms boot-time cleanup drops only this
// hub's leftover rows.
func TestRegistry_ClearStale(t *testing.T) {
	t.Parallel()

	dir := directory.NewMemoryDirectory()
	ctx := context.Background()

	// Simulate rows left by a previous incarnation of hub-1 and a
	// live row owned by hub-2.
	_ = dir.Attach(ctx, directory.PeerRecord{Peer: "ghost", Hub: "hub-1"})
	_ = dir.Attach(ctx, directory.PeerRecord{Peer: "other", Hub: "hub-2"})

	r := registry.NewRegistry(zap.NewNop(), registry.WithHubID("hub-1"), registry.WithDirectory(dir))
	if err := r.ClearStale(ctx); err != nil {
		t.Fatalf("ClearStale: %v", err)
	}

	if _, ok, _ := dir.Lookup(ctx, "ghost"); ok {
		t.Fatal("stale hub-1 row survived ClearStale")
	}
	if _, ok, _ := dir.Lookup(ctx, "other"); !ok {
		t.Fatal("ClearStale wrongly dropped another hub's row")
	}
}

// TestRegistry_MetricsNoopByDefault confirms the OTel path is safe
// without an SDK installed: recording against the global no-op
// provider must not panic.
func TestRegistry_MetricsNoopByDefault(t *testing.T) {
	t.Parallel()

	r := registry.NewRegistry(zap.NewNop())

	detach := r.Attach("p", "v1", nil, func(string) {})
	if r.CountTunnels() != 1 {
		t.Fatal("attach not recorded")
	}

	if !r.StopTunnel("p", "test") {
		t.Fatal("StopTunnel reported no tunnel")
	}
	detach("test")

	if r.CountTunnels() != 0 {
		t.Fatal("detach not recorded")
	}
}
