// Command client is a standalone holt peer. It dials the hub,
// authenticates with a bearer token, and serves an HTTP handler back
// over the reverse tunnel — while listening on nothing itself. The
// hub (and anyone curling its proxy) can then reach this peer's
// handler through the tunnel.
//
//	go run ./examples/authenticated/client --token tok-alice
//	go run ./examples/authenticated/client --token tok-bob --hub ws://127.0.0.1:7200
//
// The peer keeps running until Ctrl-C; it redials automatically if the
// hub restarts. Try an unknown token: the hub answers 401 at the
// upgrade and the peer never attaches.
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

	"github.com/openotters/holt"
)

func main() {
	hubURL := flag.String("hub", "ws://127.0.0.1:7200", "hub tunnel URL (ws or wss)")
	token := flag.String("token", "tok-alice", "bearer token identifying this peer to the hub")
	flag.Parse()

	logger, _ := zap.NewDevelopment()
	defer func() { _ = logger.Sync() }()

	if err := run(*hubURL, *token, logger); err != nil {
		log.Fatal(err)
	}
}

func run(hubURL, token string, logger *zap.Logger) error {
	// The handler this peer serves over the tunnel. None of this is
	// reachable directly — the peer opens no listener.
	mux := http.NewServeMux()
	mux.HandleFunc("/hello", helloHandler(token))
	mux.HandleFunc("/time", timeHandler)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("attaching to hub", zap.String("hub", hubURL))

	// The peer owns its connection to the hub, here a plaintext ws://
	// WebSocket with the bearer token on the upgrade request. For
	// transport TLS, dial wss:// instead.
	//
	// Run blocks, redialing with backoff, until ctx ends or the
	// hub sends a terminal GoAway.
	if err := holt.NewClient(
		hubURL,
		mux,
		holt.WithBearerToken(token),
		holt.WithVersion("authenticated-demo"),
		holt.WithLogger(logger),
	).Run(ctx); err != nil && ctx.Err() == nil {
		return err
	}

	logger.Info("peer stopped")

	return nil
}

func timeHandler(w http.ResponseWriter, _ *http.Request) {
	_, _ = fmt.Fprintf(w, "%s\n", time.Now().Format(time.RFC3339Nano))
}

func helloHandler(token string) func(w http.ResponseWriter, _ *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "hello from %s (pid %d)\n", token, os.Getpid())
	}
}
