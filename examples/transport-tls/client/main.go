// Command client is a holt peer that dials the hub over MUTUAL
// TLS. It mints a client certificate carrying its own name (signed by
// the shared demo CA), presents it on the wss:// connection, and
// verifies the hub's server certificate against the same CA. Its
// identity at the hub is that certificate's Common Name, nothing it
// asserts in a header.
//
// It serves an HTTP handler over the tunnel and listens on nothing.
//
//	go run ./examples/transport-tls/client --name alice
//	go run ./examples/transport-tls/client --name bob
//
// Run the server first (it generates the shared certs).
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"github.com/openotters/holt/dial"
	"github.com/openotters/holt/examples/certs"
)

func main() {
	hubURL := flag.String("hub", "wss://127.0.0.1:7100", "hub tunnel URL (mutual TLS, wss)")
	name := flag.String("name", "alice", "this peer's identity (its client-cert Common Name)")
	certsDir := flag.String("certs", certs.DefaultDir(), "directory holding the demo CA")
	flag.Parse()

	if err := run(*hubURL, *name, *certsDir); err != nil {
		log.Fatal(err)
	}
}

func run(hubURL, name, certsDir string) error {
	logger, _ := zap.NewDevelopment()
	defer func() { _ = logger.Sync() }()

	caPool, err := certs.LoadCA(certsDir)
	if err != nil {
		return err
	}

	// Mint a client cert carrying THIS peer's name; the hub reads the
	// CN as the authenticated identity.
	clientCert, err := certs.Issue(certsDir, name)
	if err != nil {
		return err
	}

	// Mutual TLS on the outer hop: present our client cert, verify the
	// hub's "hub" server cert via the shared CA. The custom pool rides
	// in through the HTTP client the WebSocket upgrade uses.
	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      caPool,
		ServerName:   certs.Hub,
		MinVersion:   tls.VersionTLS13,
	}}}

	mux := http.NewServeMux()
	mux.HandleFunc("/hello", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "hello from %s (pid %d, mutually authenticated)", name, os.Getpid())
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("attaching to hub over mutual TLS", zap.String("hub", hubURL), zap.String("name", name))

	if err := dial.Run(ctx, dial.Options{
		URL:        hubURL,
		HTTPClient: httpClient,
		Handler:    mux,
		Version:    "transport-tls-demo",
		Logger:     logger,
	}); err != nil && ctx.Err() == nil {
		return err
	}

	return nil
}
