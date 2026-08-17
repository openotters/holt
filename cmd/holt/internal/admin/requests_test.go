package admin_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"go.uber.org/zap"

	holtv1 "github.com/openotters/holt/api/v1"
	"github.com/openotters/holt/api/v1/holtv1connect"
	"github.com/openotters/holt/cmd/holt/internal/admin"
	"github.com/openotters/holt/pkg/registry"
	"github.com/openotters/holt/pkg/reqlog"
)

// The console's live view over a real round-trip: a watcher gets the
// requests published before it subscribed (the small in-memory window,
// which is all the hub keeps) and then whatever happens next.
func TestWatchRequests(t *testing.T) {
	t.Parallel()

	broker := reqlog.NewBroker(0)
	svc := admin.NewService(registry.NewRegistry(zap.NewNop()), admin.WithRequests(broker))

	srv := serve(t, svc)

	broker.Publish(reqlog.Event{
		At: time.Now(), Peer: "alice", Method: http.MethodGet, Path: "/before",
		Status: http.StatusOK, Duration: 12 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := holtv1connect.NewAdminClient(srv.Client(), srv.URL)

	stream, err := client.WatchRequests(ctx, connect.NewRequest(&holtv1.WatchRequestsRequest{}))
	if err != nil {
		t.Fatalf("watch: %v", err)
	}

	next := func() *holtv1.RequestEvent {
		t.Helper()

		if !stream.Receive() {
			t.Fatalf("stream ended early: %v", stream.Err())
		}

		return stream.Msg()
	}

	replayed := next()
	if replayed.GetPeer() != "alice" || replayed.GetPath() != "/before" {
		t.Fatalf("replayed = %+v, want alice /before", replayed)
	}

	if replayed.GetStatus() != http.StatusOK || replayed.GetDurationUs() != 12_000 {
		t.Errorf("replayed status=%d duration=%dµs, want 200 / 12000µs",
			replayed.GetStatus(), replayed.GetDurationUs())
	}

	broker.Publish(reqlog.Event{
		At: time.Now(), Peer: "bob", Method: http.MethodPost, Path: "/after",
		Status: http.StatusBadGateway,
	})

	if live := next(); live.GetPeer() != "bob" || live.GetPath() != "/after" {
		t.Fatalf("live = %+v, want bob /after", live)
	}
}

// Naming a peer streams that peer and nobody else: the filtering
// happens here, so a console watching one peer of a fleet is never
// sent the rest to throw away.
func TestWatchRequestsFiltersByPeer(t *testing.T) {
	t.Parallel()

	broker := reqlog.NewBroker(0)
	svc := admin.NewService(registry.NewRegistry(zap.NewNop()), admin.WithRequests(broker))
	srv := serve(t, svc)

	broker.Publish(reqlog.Event{At: time.Now(), Peer: "alice", Path: "/alice-one"})
	broker.Publish(reqlog.Event{At: time.Now(), Peer: "bob", Path: "/bob-one"})
	broker.Publish(reqlog.Event{At: time.Now(), Peer: "alice", Path: "/alice-two"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := holtv1connect.NewAdminClient(srv.Client(), srv.URL)

	stream, err := client.WatchRequests(ctx,
		connect.NewRequest(&holtv1.WatchRequestsRequest{Peer: "alice"}))
	if err != nil {
		t.Fatalf("watch: %v", err)
	}

	for _, want := range []string{"/alice-one", "/alice-two"} {
		if !stream.Receive() {
			t.Fatalf("stream ended early: %v", stream.Err())
		}

		got := stream.Msg()
		if got.GetPeer() != "alice" || got.GetPath() != want {
			t.Fatalf("got %s %s, want alice %s", got.GetPeer(), got.GetPath(), want)
		}
	}
}

// A hub wired without a broker says so, rather than holding a stream
// open that will never send.
func TestWatchRequestsUnimplemented(t *testing.T) {
	t.Parallel()

	srv := serve(t, admin.NewService(registry.NewRegistry(zap.NewNop())))
	client := holtv1connect.NewAdminClient(srv.Client(), srv.URL)

	stream, err := client.WatchRequests(context.Background(),
		connect.NewRequest(&holtv1.WatchRequestsRequest{}))
	if err == nil {
		stream.Receive()
		err = stream.Err()
	}

	if connect.CodeOf(err) != connect.CodeUnimplemented {
		t.Fatalf("err = %v, want unimplemented", err)
	}
}

// serve mounts the Admin service on a throwaway HTTP server.
func serve(t *testing.T, svc *admin.Service) *httptest.Server {
	t.Helper()

	path, handler := holtv1connect.NewAdminHandler(svc)
	mux := http.NewServeMux()
	mux.Handle(path, handler)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}
