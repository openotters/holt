package holt_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/openotters/holt/api/v1/holtv1connect"
	"github.com/openotters/holt/dial"
	"github.com/openotters/holt/hub"
)

// TestReverseTunnel_EndToEnd is the module's headline proof: a peer
// that only dials OUT serves an http.Handler that the hub reaches by
// dialing back THROUGH the tunnel.
//
// It mirrors production wiring — the hub's Connect handler on an h2c
// listener, the peer attaching with a gRPC client (Connect serves
// the gRPC protocol natively) — then issues an HTTP GET from the hub
// side and asserts the peer's handler answered.
func TestReverseTunnel_EndToEnd(t *testing.T) {
	t.Parallel()

	logger := zap.NewNop()
	registry := hub.NewRegistry(logger)

	// The hub identifies every peer on this listener as "peer-1"
	// (a real deployment reads a JWT claim / mTLS SAN here).
	identity := func(context.Context) (string, error) { return "peer-1", nil }
	path, handler := holtv1connect.NewTunnelHandler(hub.NewHandler(registry, identity, logger))

	mux := http.NewServeMux()
	mux.Handle(path, handler)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	var protocols http.Protocols
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		Protocols:         &protocols,
	}
	go func() { _ = srv.Serve(lis) }()
	defer func() { _ = srv.Close() }()

	cc, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = cc.Close() }()

	// The peer's handler — what the hub reaches through the tunnel.
	peerHandler := http.NewServeMux()
	peerHandler.HandleFunc("/ping", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("pong from peer"))
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = dial.Run(ctx, dial.Options{
			Conn:    cc,
			Handler: peerHandler,
			Version: "test",
			Logger:  logger,
		})
	}()

	waitAttached(t, registry, "peer-1")

	// Dial the peer's /ping THROUGH the tunnel.
	client := &http.Client{Transport: registry.RoundTripper("peer-1")}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://peer.invalid/ping", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("round-trip through tunnel: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "pong from peer" {
		t.Fatalf("body = %q, want pong from peer", body)
	}
}

func waitAttached(t *testing.T, r *hub.Registry, peer string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if r.Attached(peer) {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("peer never attached")
}
