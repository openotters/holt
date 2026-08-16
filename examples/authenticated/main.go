// Command authenticated shows the identity seam: the hub derives the
// peer's ID from a bearer token (a stand-in for a JWT claim or mTLS
// SAN), so the RoundTripper is keyed by an authenticated identity the
// peer cannot spoof — the attach handshake itself carries none.
//
// The code is split the way a real deployment is: server.go is the
// hub half (WithAuthBearer and the token check), client.go the peer
// half (WithBearerToken), and this file only wires the demo together.
//
// One peer attaches with a valid token and is reached by the identity
// its token proves; a peer with no token is rejected at the upgrade.
//
// Run:
//
//	go run ./examples/authenticated
//
// Expected output:
//
//	hub → alice   ⇒  "hello from alice"
//	hub → mallory ⇒  attached=false (rejected at the upgrade)
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

	// Alice attaches with her token and serves a greeting (client.go).
	startPeer(ctx, "tok-alice", "alice")

	if err := waitAttached(ctx, srv, "alice"); err != nil {
		return err
	}

	body, err := getThroughTunnel(ctx, srv, "alice")
	if err != nil {
		return err
	}

	fmt.Printf("hub → alice   ⇒  %q\n", body)

	// Mallory has no token: rejected at the upgrade (HTTP 401), never
	// lands in the registry.
	startPeer(ctx, "", "mallory")
	time.Sleep(200 * time.Millisecond)
	fmt.Printf("hub → mallory ⇒  attached=%v (rejected at the upgrade)\n", srv.Registry().Attached("mallory"))

	return nil
}

func getThroughTunnel(ctx context.Context, srv *holt.Server, peer string) (string, error) {
	client := &http.Client{Transport: srv.Registry().RoundTripper(peer)}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://peer.invalid/hello", nil)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	return string(body), nil
}

func waitAttached(ctx context.Context, srv *holt.Server, peer string) error {
	for !srv.Registry().Attached(peer) {
		select {
		case <-ctx.Done():
			return fmt.Errorf("peer %q never attached: %w", peer, ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}

	return nil
}
