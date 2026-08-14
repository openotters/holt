package holt_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/openotters/holt/dial"
	"github.com/openotters/holt/hub"
)

// TestReverseTunnel_EndToEnd is the module's headline proof: a peer
// that only dials OUT serves an http.Handler that the hub reaches by
// dialing back THROUGH the tunnel.
//
// It mirrors production wiring — the hub's WebSocket attach handler
// on a plain HTTP listener, the peer attaching with dial.Run — then
// issues an HTTP GET from the hub side and asserts the peer's
// handler answered.
func TestReverseTunnel_EndToEnd(t *testing.T) {
	t.Parallel()

	logger := zap.NewNop()
	registry := hub.NewRegistry(logger)

	// The hub identifies every peer on this listener as "peer-1"
	// (a real deployment reads a JWT claim from the upgrade request).
	identity := func(context.Context) (string, error) { return "peer-1", nil }

	mux := http.NewServeMux()
	mux.Handle("/", hub.NewHandler(registry, identity, logger))

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(lis) }()
	defer func() { _ = srv.Close() }()

	// The peer's handler — what the hub reaches through the tunnel.
	peerHandler := http.NewServeMux()
	peerHandler.HandleFunc("/ping", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("pong from peer"))
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = dial.Run(ctx, dial.Options{
			URL:     "ws://" + lis.Addr().String(),
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

// TestReverseTunnel_WSSAndKeepalive attaches through a TLS edge
// (wss://, like an ingress or CDN in front of the hub) with an
// aggressive keepalive, then round-trips again after several ping
// intervals — proving the pings keep a quiet tunnel attached instead
// of tearing it down.
func TestReverseTunnel_WSSAndKeepalive(t *testing.T) {
	t.Parallel()

	logger := zap.NewNop()
	registry := hub.NewRegistry(logger)
	identity := func(context.Context) (string, error) { return "peer-tls", nil }

	mux := http.NewServeMux()
	mux.Handle("/", hub.NewHandler(registry, identity, logger))

	// httptest's TLS server is the stand-in for the TLS edge: it
	// terminates TLS and speaks plain HTTP/1.1 to the handler, exactly
	// like an ingress or Cloudflare in front of the hub.
	edge := httptest.NewTLSServer(mux)
	defer edge.Close()

	peerHandler := http.NewServeMux()
	peerHandler.HandleFunc("/ping", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("pong"))
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = dial.Run(ctx, dial.Options{
			URL: "wss://" + edge.Listener.Addr().String(),
			// The edge's cert is self-signed; the test server's own
			// client already trusts it, the way a real deployment
			// trusts its edge via system roots.
			HTTPClient: edge.Client(),
			Keepalive:  50 * time.Millisecond,
			Handler:    peerHandler,
			Version:    "test",
			Logger:     logger,
		})
	}()

	waitAttached(t, registry, "peer-tls")

	get := func() string {
		t.Helper()

		client := &http.Client{Transport: registry.RoundTripper("peer-tls"), Timeout: 2 * time.Second}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://peer.invalid/ping", nil)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("round-trip through tunnel: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		body, _ := io.ReadAll(resp.Body)

		return string(body)
	}

	if got := get(); got != "pong" {
		t.Fatalf("body = %q, want pong", got)
	}

	// A quiet stretch spanning many keepalive intervals; the tunnel
	// must still be attached and serving afterwards.
	time.Sleep(500 * time.Millisecond)

	if !registry.Attached("peer-tls") {
		t.Fatal("tunnel dropped during a quiet keepalive stretch")
	}

	if got := get(); got != "pong" {
		t.Fatalf("after keepalive stretch: body = %q, want pong", got)
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
