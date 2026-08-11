// Command client joins the hub using a COPY-PASTE TOKEN — no cert
// files, no shared filesystem. The token (printed by the server)
// carries the CA plus this peer's client certificate and key; the
// client decodes it, dials the hub over mutual TLS, and serves its
// handler over the tunnel.
//
//	go run ./examples/join-token/client --token <paste-from-server>
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

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/openotters/holt/dial"
	"github.com/openotters/holt/examples/certs"
)

func main() {
	hubAddr := flag.String("hub", "127.0.0.1:7400", "hub mutual-TLS tunnel address")
	token := flag.String("token", "", "join token printed by the server (required)")
	flag.Parse()

	if *token == "" {
		log.Fatal("--token is required; copy it from the server's output")
	}

	if err := run(*hubAddr, *token); err != nil {
		log.Fatal(err)
	}
}

func run(hubAddr, token string) error {
	logger, _ := zap.NewDevelopment()
	defer func() { _ = logger.Sync() }()

	bundle, err := certs.DecodeBundle(token)
	if err != nil {
		return err
	}

	// Build the mutual-TLS client config straight from the token —
	// present our cert, verify the hub's "hub" server cert via the
	// bundled CA. Nothing read from disk.
	tlsCfg, err := bundle.ClientTLS(certs.Hub)
	if err != nil {
		return err
	}

	cc, err := grpc.NewClient(hubAddr, grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
	if err != nil {
		return err
	}
	defer func() { _ = cc.Close() }()

	mux := http.NewServeMux()
	mux.HandleFunc("/hello", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "hello from a token-joined peer (pid %d)", os.Getpid())
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("attaching with join token", zap.String("hub", hubAddr))

	if err := dial.Run(ctx, dial.Options{
		Conn:    cc,
		Handler: mux,
		Version: "join-token-demo",
		Logger:  logger,
	}); err != nil && ctx.Err() == nil {
		return err
	}

	return nil
}
