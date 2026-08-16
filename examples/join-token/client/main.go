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

	"github.com/openotters/holt"
	"github.com/openotters/holt/examples/certs"
)

func main() {
	hubURL := flag.String("hub", "wss://127.0.0.1:7400", "hub mutual-TLS tunnel URL (wss)")
	token := flag.String("token", "", "join token printed by the server (required)")
	flag.Parse()

	if *token == "" {
		log.Fatal("--token is required; copy it from the server's output")
	}

	if err := run(*hubURL, *token); err != nil {
		log.Fatal(err)
	}
}

func run(hubURL, token string) error {
	logger, _ := zap.NewDevelopment()
	defer func() { _ = logger.Sync() }()

	bundle, err := certs.DecodeBundle(token)
	if err != nil {
		return err
	}

	// Build the mutual-TLS client config straight from the token:
	// present our cert, verify the hub's "hub" server cert via the
	// bundled CA. Nothing read from disk.
	tlsCfg, err := bundle.ClientTLS(certs.Hub)
	if err != nil {
		return err
	}

	// The wss:// dial goes through this client, so the WebSocket
	// upgrade itself runs under the token's mutual TLS.
	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsCfg}}

	mux := http.NewServeMux()
	mux.HandleFunc("/hello", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "hello from a token-joined peer (pid %d)", os.Getpid())
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("attaching with join token", zap.String("hub", hubURL))

	if err := holt.NewClient(hubURL, mux,
		holt.WithHTTPClient(httpClient),
		holt.WithVersion("join-token-demo"),
		holt.WithLogger(logger),
	).Run(ctx); err != nil && ctx.Err() == nil {
		return err
	}

	return nil
}
