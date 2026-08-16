// Command echo is the smallest end-to-end holt demo: it stands up
// a hub and a peer in one process, the peer serves an HTTP handler
// that only it could serve, and the hub reaches that handler by
// dialing back THROUGH the tunnel the peer opened.
//
// Nothing listens on the peer. The only inbound listener in the whole
// program is the hub's — exactly the point of a reverse tunnel.
//
// The hub is holt.New with no identity configured, so the
// development identity applies: the peer names itself with the
// x-holt-peer header, nothing verifies the claim, and the tunnel must
// stay on loopback (see the `authenticated` example for a real
// identity).
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

	"github.com/openotters/holt"
	"github.com/openotters/holt/pkg/registry"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	logger := zap.NewNop()

	// ── Hub ────────────────────────────────────────────────────────
	// One call: the tunnel endpoint on a loopback listener, no proxy.
	// No identity is configured, so peers name themselves (development
	// identity, loopback only).
	var lc net.ListenConfig

	lis, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}

	srv := holt.NewServer(
		holt.WithLogger(logger),
		holt.WithTunnel(holt.NewTunnel("", holt.WithListener(lis))),
		holt.WithProxy(nil),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	runDone := make(chan error, 1)
	go func() { runDone <- srv.Run(ctx) }()

	// ── Peer ───────────────────────────────────────────────────────
	// Dials the hub and serves its handler back over that connection.
	// The peer never listens.
	peerMux := http.NewServeMux()
	peerMux.HandleFunc("/whoami", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("I am the peer; the hub reached me through the tunnel"))
	})

	go func() {
		_ = holt.NewClient("ws://"+lis.Addr().String(), peerMux,
			holt.WithHeader(holt.DevPeerHeader, "peer"),
			holt.WithVersion("echo-demo"),
			holt.WithLogger(logger),
		).Run(ctx)
	}()

	// ── Wait for attach, then dial the peer through the tunnel ──────
	if err := waitAttached(ctx, srv.Registry(), "peer"); err != nil {
		return err
	}

	client := &http.Client{Transport: srv.Registry().RoundTripper("peer")}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://peer.invalid/whoami", nil)

	resp, doErr := client.Do(req)
	if doErr != nil {
		return doErr
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("hub → peer GET /whoami  ⇒  %d  %q\n", resp.StatusCode, body)

	cancel()
	<-runDone

	return nil
}

func waitAttached(ctx context.Context, r *registry.Registry, peer string) error {
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
