// Command client is a standalone holt peer. It dials the hub,
// authenticates with a bearer token, and serves an HTTP handler back
// over the reverse tunnel — while listening on nothing itself. The
// hub (and anyone using the hub's operator API) can then reach this
// peer's handler through the tunnel.
//
//	go run ./examples/client-server/client --token tok-alice
//	go run ./examples/client-server/client --token tok-bob --hub 127.0.0.1:7000
//
// The peer keeps running until Ctrl-C; it redials automatically if the
// hub restarts.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"github.com/openotters/holt/dial"
)

func main() {
	hubAddr := flag.String("hub", "127.0.0.1:7000", "hub tunnel (gRPC) address")
	token := flag.String("token", "tok-alice", "bearer token identifying this peer to the hub")
	flag.Parse()

	if err := run(*hubAddr, *token); err != nil {
		log.Fatal(err)
	}
}

func run(hubAddr, token string) error {
	logger, _ := zap.NewDevelopment()
	defer func() { _ = logger.Sync() }()

	// The handler this peer serves over the tunnel. None of this is
	// reachable directly — the peer opens no listener.
	mux := http.NewServeMux()
	mux.HandleFunc("/hello", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "hello from %s (pid %d)\n", token, os.Getpid())
	})
	mux.HandleFunc("/time", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "%s\n", time.Now().Format(time.RFC3339Nano))
	})
	mux.HandleFunc("/env", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "requested-path: %s\n", r.URL.Path)
	})

	// The peer owns its connection to the hub — here plaintext with a
	// bearer token. For transport TLS, dial with
	// credentials.NewTLS(...) instead (see ../../transport-tls).
	cc, err := grpc.NewClient(hubAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(bearerUnary(token)),
		grpc.WithStreamInterceptor(bearerStream(token)))
	if err != nil {
		return err
	}
	defer func() { _ = cc.Close() }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("attaching to hub", zap.String("hub", hubAddr))

	// dial.Run blocks, redialing with backoff, until ctx ends or the
	// hub sends a terminal GoAway.
	if err := dial.Run(ctx, dial.Options{
		Conn:    cc,
		Handler: mux,
		Version: "client-server-demo",
		Logger:  logger,
	}); err != nil && ctx.Err() == nil {
		return err
	}

	logger.Info("peer stopped")

	return nil
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
