// Command server is a holt hub whose OUTER WebSocket hop is
// plaintext, but which runs MUTUAL TLS INSIDE each tunnel. After the plaintext
// holt handshake it becomes the inner TLS client: it presents the
// "hub" certificate and verifies the peer's inner server certificate
// against a shared CA. The peer, in turn, requires the hub's client
// certificate — so both processes authenticate each other and the
// payload is encrypted end-to-end, even though the transport is
// plaintext (as it would be past a TLS-terminating proxy).
//
// On the first run it generates a demo CA + certs into a shared temp
// dir; start it before the client.
//
//	go run ./examples/encrypted/server
//	go run ./examples/encrypted/client   # in another terminal
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/openotters/holt/examples/certs"
	"github.com/openotters/holt/hub"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:7200", "tunnel (WebSocket) listen address; transport is plaintext")
	certsDir := flag.String("certs", certs.DefaultDir(), "directory for the demo CA + certificates")
	flag.Parse()

	if err := run(*addr, *certsDir); err != nil {
		log.Fatal(err)
	}
}

func run(addr, certsDir string) error {
	logger, _ := zap.NewDevelopment()
	defer func() { _ = logger.Sync() }()

	if err := certs.EnsureDir(certsDir); err != nil {
		return err
	}

	logger.Info("demo certs ready", zap.String("dir", certsDir))

	hubCert, err := certs.Load(certsDir, certs.Hub)
	if err != nil {
		return err
	}

	caPool, err := certs.LoadCA(certsDir)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// WithPeerTLS makes the hub the INNER TLS client: present the hub
	// cert, verify the peer's inner server cert ("peer") via the CA.
	innerTLS := &tls.Config{
		RootCAs:      caPool,
		ServerName:   certs.Peer,
		Certificates: []tls.Certificate{hubCert},
		MinVersion:   tls.VersionTLS13,
	}

	// Outer transport is a plaintext WebSocket; the inner TLS protects
	// the payload regardless. Single-peer demo, so the routing
	// identity is fixed — the security here is the inner mutual TLS,
	// not identity routing.
	srv := hub.NewServer(
		hub.WithLogger(logger),
		hub.WithTunnel(hub.NewTunnel(addr,
			hub.WithIdentity(func(context.Context) (string, error) { return "peer", nil }),
			hub.WithHandlerOptions(hub.WithPeerTLS(innerTLS)),
		)),
		hub.WithProxy(nil),
	)

	greetOnAttach(ctx, srv.Registry(), logger)

	logger.Info("hub up (plaintext transport, inner mutual TLS)", zap.String("addr", addr))

	return srv.Run(ctx)
}

// greetOnAttach reaches the peer through the (inner-encrypted) tunnel
// on attach and logs the reply.
func greetOnAttach(ctx context.Context, registry *hub.Registry, logger *zap.Logger) {
	events := registry.Watch(ctx)

	go func() {
		for ev := range events {
			if ev.Kind != hub.EventAttached {
				continue
			}

			reply, err := get(ctx, registry, ev.Peer, "/secret")
			if err != nil {
				logger.Warn("reach peer failed", zap.String("peer", ev.Peer), zap.Error(err))

				continue
			}

			logger.Info("reached peer through inner-TLS tunnel",
				zap.String("peer", ev.Peer), zap.String("reply", reply))
		}
	}()
}

func get(ctx context.Context, registry *hub.Registry, peer, path string) (string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// https because the hub speaks TLS into the tunnel; the host is a
	// placeholder the RoundTripper ignores.
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "https://peer.invalid"+path, nil)
	if err != nil {
		return "", err
	}

	client := &http.Client{Transport: registry.RoundTripper(peer)}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	return string(body), nil
}
