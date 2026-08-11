// Command client is a holt peer that dials the hub over a
// PLAINTEXT transport but serves its handler over MUTUAL TLS INSIDE
// the tunnel. It becomes the inner TLS server: it presents the "peer"
// certificate and REQUIRES the hub's client certificate, both verified
// against the shared demo CA. So even though the gRPC hop is plaintext
// — as it would be after a TLS-terminating proxy — the payload is
// encrypted and both ends are cryptographically authenticated.
//
// It listens on nothing.
//
//	go run ./examples/encrypted/client
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
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/openotters/holt/dial"
	"github.com/openotters/holt/examples/certs"
)

func main() {
	hubAddr := flag.String("hub", "127.0.0.1:7200", "hub tunnel address (plaintext transport)")
	certsDir := flag.String("certs", certs.DefaultDir(), "directory holding the demo CA + certs")
	flag.Parse()

	if err := run(*hubAddr, *certsDir); err != nil {
		log.Fatal(err)
	}
}

func run(hubAddr, certsDir string) error {
	logger, _ := zap.NewDevelopment()
	defer func() { _ = logger.Sync() }()

	peerCert, err := certs.Load(certsDir, certs.Peer)
	if err != nil {
		return err
	}

	caPool, err := certs.LoadCA(certsDir)
	if err != nil {
		return err
	}

	// Inner TLS server config: present the peer cert AND require the
	// hub's client cert, both anchored to the shared CA.
	innerTLS := &tls.Config{
		Certificates: []tls.Certificate{peerCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    caPool,
		MinVersion:   tls.VersionTLS13,
	}

	// Outer transport is plaintext; inner TLS does the protecting.
	cc, err := grpc.NewClient(hubAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer func() { _ = cc.Close() }()

	mux := http.NewServeMux()
	mux.HandleFunc("/secret", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "secret from peer (pid %d), mutually authenticated inside the tunnel", os.Getpid())
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("attaching (plaintext transport, inner mutual TLS)", zap.String("hub", hubAddr))

	if err := dial.Run(ctx, dial.Options{
		Conn:      cc,
		Handler:   mux,
		TLSConfig: innerTLS,
		Version:   "encrypted-demo",
		Logger:    logger,
	}); err != nil && ctx.Err() == nil {
		return err
	}

	return nil
}
