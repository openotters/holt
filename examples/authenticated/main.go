// Command authenticated shows the identity seam: the hub derives each
// peer's ID from a bearer token (a stand-in for a JWT claim or mTLS
// SAN), so the RoundTripper is keyed by an authenticated identity the
// peer cannot spoof (the attach upgrade carries no identity at all).
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
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/openotters/holt/dial"
	"github.com/openotters/holt/hub"
)

// tokens maps a demo bearer token to the peer identity it proves. A
// real hub validates a JWT signature or an mTLS certificate here.
var tokens = map[string]string{
	"tok-alice": "alice",
	"tok-bob":   "bob",
}

// peerCtxKey carries the authenticated peer ID from the auth
// middleware to the hub's Identity func.
type peerCtxKey struct{}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	logger := zap.NewNop()
	registry := hub.NewRegistry(logger)

	// The hub reads the peer ID the auth middleware stamped on the
	// context. This is the whole identity seam: the handshake never
	// carries an ID, so a peer cannot claim to be someone else.
	identity := func(ctx context.Context) (string, error) {
		peer, _ := ctx.Value(peerCtxKey{}).(string)
		if peer == "" {
			return "", errors.New("no authenticated peer")
		}

		return peer, nil
	}

	mux := http.NewServeMux()
	mux.Handle("/", authMiddleware(hub.NewHandler(registry, identity, logger)))

	var lc net.ListenConfig
	lis, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(lis) }()
	defer func() { _ = srv.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Two authenticated peers, each serving a handler that greets in
	// its own name.
	for _, name := range []string{"alice", "bob"} {
		startPeer(ctx, lis.Addr().String(), "tok-"+name, name, logger)
	}

	for _, name := range []string{"alice", "bob"} {
		if waitErr := waitAttached(ctx, registry, name); waitErr != nil {
			return waitErr
		}

		body, getErr := getThroughTunnel(ctx, registry, name)
		if getErr != nil {
			return getErr
		}

		fmt.Printf("hub → %-7s ⇒  %q\n", name, body)
	}

	// A peer with no token is rejected at the upgrade (HTTP 401), so
	// it never lands in the registry.
	startPeer(ctx, lis.Addr().String(), "", "unknown", logger)
	time.Sleep(200 * time.Millisecond)
	fmt.Printf("hub → unknown ⇒  attached=%v (rejected at the upgrade)\n", registry.Attached("unknown"))

	return nil
}

// authMiddleware validates the bearer token on the WebSocket upgrade
// request and stamps the resolved peer ID onto the request context,
// so the hub's Identity func can read it.
func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer, ok := tokenFromHeader(r.Header.Get("Authorization"))
		if !ok {
			http.Error(w, "invalid or missing bearer token", http.StatusUnauthorized)

			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), peerCtxKey{}, peer)))
	})
}

// startPeer dials the hub with a bearer token on the upgrade request
// and serves a name-greeting handler over the tunnel.
func startPeer(ctx context.Context, hubAddr, token, name string, logger *zap.Logger) {
	peerMux := http.NewServeMux()
	peerMux.HandleFunc("/hello", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "hello from %s", name)
	})

	var header http.Header
	if token != "" {
		header = http.Header{"Authorization": {"Bearer " + token}}
	}

	go func() {
		_ = dial.Run(ctx, dial.Options{
			URL:     "ws://" + hubAddr,
			Header:  header,
			Handler: peerMux,
			Version: name,
			Logger:  logger,
		})
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

// tokenFromHeader resolves a bearer token to a peer ID.
func tokenFromHeader(h string) (string, bool) {
	tok := strings.TrimPrefix(h, "Bearer ")
	peer, ok := tokens[tok]

	return peer, ok
}
