// Command client is the peer half of the echo example: it dials the
// hub and serves an HTTP handler back over the reverse tunnel, while
// listening on nothing itself.
//
// Under the hub's development identity the peer names itself with the
// x-holt-peer header; nothing verifies the claim (loopback demos
// only).
//
//	go run ./examples/echo/client
//
// The peer keeps running until Ctrl-C; it redials automatically if
// the hub restarts.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"github.com/openotters/holt"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger, _ := zap.NewDevelopment()
	defer func() { _ = logger.Sync() }()

	// The handler this peer serves over the tunnel. None of it is
	// reachable directly — the peer opens no listener.
	handler := http.NewServeMux()
	handler.HandleFunc("/whoami", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("I am the peer; the hub reached me through the tunnel\n"))
	})

	// The whole peer: attach, serve, redial with backoff.
	err := holt.NewClient("ws://"+holt.DefaultTunnelAddr, handler,
		holt.WithHeader(holt.DevPeerHeader, "peer"),
		holt.WithLogger(logger),
	).Run(ctx)
	if err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}
