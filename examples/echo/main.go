// Command echo is the smallest end-to-end holt demo: a hub and a
// peer in one process. The peer serves an HTTP handler while
// listening on nothing; the hub reaches that handler by dialing back
// THROUGH the tunnel the peer opened.
//
// The code is split the way a real deployment is: server.go is the
// hub half, client.go the peer half, and this file only wires the
// demo together.
//
// Both halves run with zero configuration: holt.NewServer() serves
// the tunnel on 127.0.0.1:7000 (and a proxy on :7002) with the
// development identity — the peer names itself with the x-holt-peer
// header, nothing verifies the claim, loopback only. See the
// `authenticated` example for a real identity.
//
// Run:
//
//	go run ./examples/echo
//
// Expected output:
//
//	hub → peer GET /whoami  ⇒  200  "I am the peer; the hub reached me through the tunnel"
//
// While it runs you can also reach the peer from a shell, through the
// hub's proxy:
//
//	curl -H 'x-tunnel-peer: peer' http://127.0.0.1:7002/whoami
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/openotters/holt"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	srv := startHub(ctx) // server.go
	startPeer(ctx)       // client.go

	// ── Reach the peer THROUGH the tunnel. ──
	if err := waitAttached(ctx, srv); err != nil {
		return err
	}

	client := &http.Client{Transport: srv.Registry().RoundTripper("peer")}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://peer.invalid/whoami", nil)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("hub → peer GET /whoami  ⇒  %d  %q\n", resp.StatusCode, body)

	return nil
}

// waitAttached polls until the peer's tunnel is up (attaching takes a
// few milliseconds).
func waitAttached(ctx context.Context, srv *holt.Server) error {
	for !srv.Registry().Attached("peer") {
		select {
		case <-ctx.Done():
			return fmt.Errorf("peer never attached: %w", ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}

	return nil
}
