// Command authenticated shows the identity seam: the hub derives the
// peer's ID from a bearer token (a stand-in for a JWT claim or mTLS
// SAN), so the RoundTripper is keyed by an authenticated identity the
// peer cannot spoof — the attach handshake itself carries none.
//
// WithAuthBearer is the whole seam for bearer tokens: the middleware
// that guards the upgrade, and the identity that keys the registry,
// both from one token-verifying func. Any other scheme is
// WithMiddleware (stamp the context) + WithIdentity (read it back).
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
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/openotters/holt"
)

// peerForToken maps a bearer token to the peer identity it proves. A
// real hub validates a JWT signature or an mTLS certificate here.
func peerForToken(_ context.Context, token string) (string, error) {
	if token != "tok-alice" {
		return "", errors.New("unknown token")
	}

	return "alice", nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// The one option that matters here: WithAuthBearer guards the
	// upgrade with the token check and keys each tunnel by the peer id
	// the token proves.
	srv := holt.NewServer(
		holt.WithTunnel(holt.NewTunnel(holt.DefaultTunnelAddr,
			holt.WithAuthBearer(peerForToken),
		)),
		holt.WithProxy(nil),
	)
	go func() { _ = srv.Run(ctx) }()

	// Alice attaches with her token and serves a greeting.
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

// startPeer dials the hub with a bearer token on the upgrade request
// and serves a name-greeting handler over the tunnel.
func startPeer(ctx context.Context, token, name string) {
	handler := http.NewServeMux()
	handler.HandleFunc("/hello", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "hello from %s", name)
	})

	var opts []holt.ClientOption
	if token != "" {
		opts = append(opts, holt.WithBearerToken(token))
	}

	go func() {
		_ = holt.NewClient("ws://"+holt.DefaultTunnelAddr, handler, opts...).Run(ctx)
	}()
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
