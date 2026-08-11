// Command authenticated shows the identity seam: the hub derives each
// peer's ID from a bearer token (a stand-in for a JWT claim or mTLS
// SAN), so the RoundTripper is keyed by an authenticated identity the
// peer cannot spoof — the Attach handshake carries no identity at all.
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
//	hub → unknown ⇒  not attached (rejected at handshake)
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

	"connectrpc.com/connect"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"github.com/openotters/holt/api/v1/holtv1connect"
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
// interceptor to the hub's Identity func.
type peerCtxKey struct{}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	logger := zap.NewNop()
	registry := hub.NewRegistry(logger)

	// The hub reads the peer ID the auth interceptor stamped on the
	// context. This is the whole identity seam: the handshake never
	// carries an ID, so a peer cannot claim to be someone else.
	identity := func(ctx context.Context) (string, error) {
		peer, _ := ctx.Value(peerCtxKey{}).(string)
		if peer == "" {
			return "", errors.New("no authenticated peer")
		}

		return peer, nil
	}

	path, handler := holtv1connect.NewTunnelHandler(
		hub.NewHandler(registry, identity, logger),
		connect.WithInterceptors(authInterceptor{}),
	)

	mux := http.NewServeMux()
	mux.Handle(path, handler)

	var lc net.ListenConfig
	lis, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}

	var protocols http.Protocols
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, Protocols: &protocols}
	go func() { _ = srv.Serve(lis) }()
	defer func() { _ = srv.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Two authenticated peers, each serving a handler that greets in
	// its own name.
	for _, name := range []string{"alice", "bob"} {
		if startErr := startPeer(ctx, lis.Addr().String(), "tok-"+name, name, logger); startErr != nil {
			return startErr
		}
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

	// A peer with no token is rejected at the handshake — it never
	// lands in the registry.
	_ = startPeer(ctx, lis.Addr().String(), "", "unknown", logger)
	time.Sleep(200 * time.Millisecond)
	fmt.Printf("hub → unknown ⇒  attached=%v (rejected at handshake)\n", registry.Attached("unknown"))

	return nil
}

// authInterceptor validates the bearer token on the (streaming)
// Attach call and stamps the resolved peer ID onto the context, so
// the hub's Identity func can read it. Attach is server-streaming
// from connect's point of view, so only WrapStreamingHandler matters.
type authInterceptor struct{}

func (authInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc { return next }

func (authInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (authInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		peer, ok := tokenFromHeader(conn.RequestHeader().Get("Authorization"))
		if !ok {
			return connect.NewError(connect.CodeUnauthenticated, errors.New("invalid or missing bearer token"))
		}

		return next(context.WithValue(ctx, peerCtxKey{}, peer), conn)
	}
}

// startPeer dials the hub with a bearer token and serves a
// name-greeting handler over the tunnel.
func startPeer(ctx context.Context, hubAddr, token, name string, logger *zap.Logger) error {
	peerMux := http.NewServeMux()
	peerMux.HandleFunc("/hello", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "hello from %s", name)
	})

	dialOpts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if token != "" {
		dialOpts = append(dialOpts,
			grpc.WithUnaryInterceptor(bearerUnary(token)),
			grpc.WithStreamInterceptor(bearerStream(token)))
	}

	cc, err := grpc.NewClient(hubAddr, dialOpts...)
	if err != nil {
		return err
	}

	go func() {
		defer func() { _ = cc.Close() }()
		_ = dial.Run(ctx, dial.Options{Conn: cc, Handler: peerMux, Version: name, Logger: logger})
	}()

	return nil
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

func bearerUnary(token string) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context, method string, req, reply any,
		cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption,
	) error {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)

		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

func bearerStream(token string) grpc.StreamClientInterceptor {
	return func(
		ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn,
		method string, streamer grpc.Streamer, opts ...grpc.CallOption,
	) (grpc.ClientStream, error) {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)

		return streamer(ctx, desc, cc, method, opts...)
	}
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
