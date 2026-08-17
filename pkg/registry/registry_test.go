package registry_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/openotters/holt/pkg/registry"
	"github.com/openotters/holt/pkg/tunneltype"
)

func TestRegistry_AttachDetach(t *testing.T) {
	t.Parallel()

	r := registry.NewRegistry(zap.NewNop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := r.Watch(ctx)
	detach := r.Attach("peer-1", "v1", tunneltype.HTTP, nil, func(string) {})

	if !r.Attached("peer-1") {
		t.Fatal("not attached after Attach")
	}

	if info, ok := r.Tunnel("peer-1"); !ok || info.PeerVersion != "v1" {
		t.Fatalf("Tunnel = %+v, %v; want version v1", info, ok)
	}
	if got := r.CountTunnels(); got != 1 {
		t.Fatalf("CountTunnels = %d, want 1", got)
	}
	if tunnels := r.ListTunnels(); len(tunnels) != 1 || tunnels[0].Peer != "peer-1" {
		t.Fatalf("ListTunnels = %+v", tunnels)
	}
	if ev := recv(t, events); ev.Kind != registry.EventAttached || ev.Peer != "peer-1" {
		t.Fatalf("event = %+v", ev)
	}

	detach("connection-lost")

	if r.Attached("peer-1") {
		t.Fatal("still attached after detach")
	}
	if ev := recv(t, events); ev.Kind != registry.EventDetached || ev.Reason != "connection-lost" {
		t.Fatalf("event = %+v", ev)
	}
}

func TestRegistry_SupersededClosesLoser(t *testing.T) {
	t.Parallel()

	r := registry.NewRegistry(zap.NewNop())

	var closedWith string

	loser := r.Attach("p", "v1", tunneltype.HTTP, nil, func(reason string) { closedWith = reason })
	_ = r.Attach("p", "v2", tunneltype.HTTP, nil, func(string) {})

	if closedWith != "superseded" {
		t.Fatalf("loser closed with %q", closedWith)
	}

	loser("connection-lost") // must not evict the winner
	if !r.Attached("p") {
		t.Fatal("winner evicted by loser's detach")
	}
}

func TestRegistry_RoundTripperDetached(t *testing.T) {
	t.Parallel()

	r := registry.NewRegistry(zap.NewNop())
	rt := r.RoundTripper("nobody")

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://peer.invalid/", nil)
	if _, err := rt.RoundTrip(req); !errors.Is(err, registry.ErrPeerDetached) {
		t.Fatalf("err = %v, want ErrPeerDetached", err)
	}
}

func recv(t *testing.T, ch <-chan registry.Event) registry.Event {
	t.Helper()

	select {
	case ev := <-ch:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("no event")

		return registry.Event{}
	}
}
