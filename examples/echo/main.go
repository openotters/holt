// Command echo is the smallest end-to-end holt demo: it stands up
// a hub and a peer in one process, the peer serves an HTTP handler
// that only it could serve, and the hub reaches that handler by
// dialing back THROUGH the tunnel the peer opened.
//
// Nothing listens on the peer. The only inbound listener in the whole
// program is the hub's — exactly the point of a reverse tunnel.
//
// Run:
//
//	go run ./examples/echo
//
// Expected output:
//
//	hub → peer GET /whoami  ⇒  200  "I am the peer; the hub reached me through the tunnel"
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/openotters/holt/dial"
	"github.com/openotters/holt/hub"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	logger := zap.NewNop()

	// ── Hub ────────────────────────────────────────────────────────
	// The registry tracks live peers; the handler accepts WebSocket
	// attachments. This demo trusts every caller and labels it "peer"
	// (see the `authenticated` example for a real identity func).
	registry := hub.NewRegistry(logger)
	identity := func(context.Context) (string, error) { return "peer", nil }

	mux := http.NewServeMux()
	mux.Handle("/", hub.NewHandler(registry, identity, logger))

	var lc net.ListenConfig
	lis, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(lis) }()
	defer func() { _ = srv.Close() }()

	// ── Peer ───────────────────────────────────────────────────────
	// Dials the hub and serves its handler back over that connection.
	// The peer never listens.
	peerMux := http.NewServeMux()
	peerMux.HandleFunc("/whoami", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("I am the peer; the hub reached me through the tunnel"))
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go func() {
		_ = dial.Run(ctx, dial.Options{
			URL:     "ws://" + lis.Addr().String(),
			Handler: peerMux,
			Version: "echo-demo",
			Logger:  logger,
		})
	}()

	// ── Wait for attach, then dial the peer through the tunnel ──────
	if err := waitAttached(ctx, registry, "peer"); err != nil {
		return err
	}

	client := &http.Client{Transport: registry.RoundTripper("peer")}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://peer.invalid/whoami", nil)

	resp, doErr := client.Do(req)
	if doErr != nil {
		return doErr
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("hub → peer GET /whoami  ⇒  %d  %q\n", resp.StatusCode, body)

	return nil
}

func waitAttached(ctx context.Context, r *hub.Registry, peer string) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		if r.Attached(peer) {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("peer %q never attached: %w", peer, ctx.Err())
		case <-ticker.C:
		}
	}
}
