// Command authenticated shows the identity seam: the hub derives each
// peer's ID from a bearer token (a stand-in for a JWT claim or mTLS
// SAN), so the RoundTripper is keyed by an authenticated identity the
// peer cannot spoof (the attach upgrade carries no identity at all).
//
// WithAuthBearer is the whole seam for bearer tokens: the middleware
// that guards the upgrade, and the identity that keys the registry,
// both from one token-verifying func. Any other scheme is
// WithMiddleware (stamp the context) + WithIdentity (read it back) —
// see the `transport-tls` example for a client-certificate one.
//
// Two peers attach with different tokens; the hub reaches each by the
// identity its token established, and an unauthenticated attach is
// rejected.
//
// Run:
//
//	go run ./examples/authenticated
//
// Expected output:
//
//	hub → alice   ⇒  "hello from alice"
//	hub → bob     ⇒  "hello from bob"
//	hub → unknown ⇒  not attached (rejected at the upgrade)
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/openotters/holt"
	"github.com/openotters/holt/hub"
)

// peerForToken maps a demo bearer token to the peer identity it
// proves. A real hub validates a JWT signature or an mTLS certificate
// here.
func peerForToken(_ context.Context, token string) (string, error) {
	peers := map[string]string{
		"tok-alice": "alice",
		"tok-bob":   "bob",
	}

	peer, ok := peers[token]
	if !ok {
		return "", errors.New("unknown token")
	}

	return peer, nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	logger := zap.NewNop()

	var lc net.ListenConfig

	lis, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}

	// The whole identity seam is the one option: WithAuthBearer guards
	// the upgrade with the token check and keys each tunnel by the
	// peer id the token proves.
	srv := holt.NewServer(
		holt.WithLogger(logger),
		holt.WithTunnel(holt.NewTunnel("",
			holt.WithListener(lis),
			holt.WithAuthBearer(peerForToken),
		)),
		holt.WithProxy(nil),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	runDone := make(chan error, 1)
	go func() { runDone <- srv.Run(ctx) }()

	// Two authenticated peers, each serving a handler that greets in
	// its own name.
	for _, name := range []string{"alice", "bob"} {
		startPeer(ctx, lis.Addr().String(), "tok-"+name, name, logger)
	}

	for _, name := range []string{"alice", "bob"} {
		if waitErr := waitAttached(ctx, srv.Registry(), name); waitErr != nil {
			return waitErr
		}

		body, getErr := getThroughTunnel(ctx, srv.Registry(), name)
		if getErr != nil {
			return getErr
		}

		fmt.Printf("hub → %-7s ⇒  %q\n", name, body)
	}

	// A peer with no token is rejected at the upgrade (HTTP 401), so
	// it never lands in the registry.
	startPeer(ctx, lis.Addr().String(), "", "unknown", logger)
	time.Sleep(200 * time.Millisecond)
	fmt.Printf("hub → unknown ⇒  attached=%v (rejected at the upgrade)\n", srv.Registry().Attached("unknown"))

	cancel()
	<-runDone

	return nil
}

// startPeer dials the hub with a bearer token on the upgrade request
// and serves a name-greeting handler over the tunnel.
func startPeer(ctx context.Context, hubAddr, token, name string, logger *zap.Logger) {
	peerMux := http.NewServeMux()
	peerMux.HandleFunc("/hello", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "hello from %s", name)
	})

	opts := []holt.ClientOption{
		holt.WithVersion(name),
		holt.WithLogger(logger),
	}
	if token != "" {
		opts = append(opts, holt.WithBearerToken(token))
	}

	go func() {
		_ = holt.NewClient("ws://"+hubAddr, peerMux, opts...).Run(ctx)
	}()
}

func getThroughTunnel(ctx context.Context, r *hub.Registry, peer string) (string, error) {
	client := &http.Client{Transport: r.RoundTripper(peer)}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://peer.invalid/hello", nil)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	return string(body), nil
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
