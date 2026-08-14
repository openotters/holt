// Command server is a holt hub that secures the OUTER WebSocket
// connection with MUTUAL TLS. Peers dial in over TLS presenting a
// client certificate; the hub verifies it against a shared CA and
// takes the peer's identity from the certificate's Common Name — so a
// peer is authenticated cryptographically, not by any header it sends.
//
// On the first run it generates a demo CA + certs into a shared temp
// dir; start it before the client.
//
//	go run ./examples/transport-tls/server
//
// Then, in another terminal:
//
//	go run ./examples/transport-tls/client --name alice
//
// The server logs when a peer attaches and immediately reaches back
// through the tunnel to prove it works.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/openotters/holt/examples/certs"
	"github.com/openotters/holt/hub"
)

type peerCtxKey struct{}

func main() {
	addr := flag.String("addr", "127.0.0.1:7100", "mutual-TLS tunnel (WebSocket) listen address")
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

	// The hub is the TLS server here: it presents the "hub" cert and
	// REQUIRES a client cert signed by the shared CA. That client cert
	// is how a peer proves who it is.
	hubCert, err := certs.Load(certsDir, certs.Hub)
	if err != nil {
		return err
	}

	caPool, err := certs.LoadCA(certsDir)
	if err != nil {
		return err
	}

	registry := hub.NewRegistry(logger, hub.WithHubID("hub"))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// On attach, reach back through the tunnel to prove the secure
	// path works end-to-end.
	greetOnAttach(ctx, registry, logger)

	identity := func(ctx context.Context) (string, error) {
		peer, _ := ctx.Value(peerCtxKey{}).(string)
		if peer == "" {
			return "", errors.New("no client-certificate identity")
		}

		return peer, nil
	}

	mux := http.NewServeMux()
	// certIdentity runs before the attach handler and lifts the
	// verified client-cert CN into the request context.
	mux.Handle("/", certIdentity(hub.NewHandler(registry, identity, logger)))

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{hubCert},
			ClientAuth:   tls.RequireAndVerifyClientCert,
			ClientCAs:    caPool,
			MinVersion:   tls.VersionTLS13,
		},
	}

	var lc net.ListenConfig

	lis, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return err
	}

	go func() {
		if serveErr := srv.ServeTLS(lis, "", ""); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			logger.Error("serve", zap.Error(serveErr))
		}
	}()

	logger.Info("hub up (mutual TLS)", zap.String("addr", addr))

	<-ctx.Done()
	registry.StopAllTunnels("shutting-down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return srv.Shutdown(shutdownCtx)
}

// certIdentity requires a verified client certificate and stamps its
// Common Name onto the request context as the peer identity.
func certIdentity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			http.Error(w, "client certificate required", http.StatusUnauthorized)

			return
		}

		cn := r.TLS.PeerCertificates[0].Subject.CommonName
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), peerCtxKey{}, cn)))
	})
}

// greetOnAttach reaches each newly-attached peer through the tunnel
// and logs the reply — the hub proving the secure round-trip works.
func greetOnAttach(ctx context.Context, registry *hub.Registry, logger *zap.Logger) {
	events := registry.Watch(ctx)

	go func() {
		for ev := range events {
			if ev.Kind != hub.EventAttached {
				continue
			}

			reply, err := get(ctx, registry, ev.Peer, "/hello")
			if err != nil {
				logger.Warn("reach peer failed", zap.String("peer", ev.Peer), zap.Error(err))

				continue
			}

			logger.Info("reached peer through tunnel (cert-authenticated)",
				zap.String("peer", ev.Peer), zap.String("reply", reply))
		}
	}()
}

func get(ctx context.Context, registry *hub.Registry, peer, path string) (string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "http://peer.invalid"+path, nil)
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
