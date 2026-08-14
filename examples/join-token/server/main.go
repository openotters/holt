// Command server is a mutual-TLS holt hub that hands out its
// client credential as a COPY-PASTE TOKEN instead of writing files.
//
// It generates a CA in memory, prints a one-line join token (CA +
// client cert + key, base64), and serves the tunnel over mutual TLS.
// You copy the token into the client's --token flag — no shared
// filesystem, no cert files.
//
//	go run ./examples/join-token/server
//	# copy the printed token, then:
//	go run ./examples/join-token/client --token <paste>
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
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
	addr := flag.String("addr", "127.0.0.1:7400", "mutual-TLS tunnel (WebSocket) listen address")
	peerName := flag.String("peer-name", "peer", "identity to mint into the client token")
	flag.Parse()

	if err := run(*addr, *peerName); err != nil {
		log.Fatal(err)
	}
}

func run(addr, peerName string) error {
	logger, _ := zap.NewDevelopment()
	defer func() { _ = logger.Sync() }()

	// Everything in memory: a fresh CA, the hub's own server cert, and
	// a client bundle to hand out.
	pki, err := certs.NewPKI()
	if err != nil {
		return err
	}

	hubCert, err := pki.ServerCert(certs.Hub)
	if err != nil {
		return err
	}

	bundle, err := pki.ClientBundle(peerName)
	if err != nil {
		return err
	}

	// Print the copy-paste token. This is the whole point — no files.
	fmt.Println("\n──────────────── client join token ────────────────")
	fmt.Printf("go run ./examples/join-token/client --token %s\n", bundle.Encode())
	fmt.Print("────────────────────────────────────────────────────\n\n")

	registry := hub.NewRegistry(logger, hub.WithHubID("hub"))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	greetOnAttach(ctx, registry, logger)

	identity := func(ctx context.Context) (string, error) {
		peer, _ := ctx.Value(peerCtxKey{}).(string)
		if peer == "" {
			return "", errors.New("no client-certificate identity")
		}

		return peer, nil
	}

	mux := http.NewServeMux()
	mux.Handle("/", certIdentity(hub.NewHandler(registry, identity, logger)))

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{hubCert},
			ClientAuth:   tls.RequireAndVerifyClientCert,
			ClientCAs:    pki.Pool(),
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

	logger.Info("hub up (mutual TLS, token-issued client cert)", zap.String("addr", addr))

	<-ctx.Done()
	registry.StopAllTunnels("shutting-down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return srv.Shutdown(shutdownCtx)
}

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

func greetOnAttach(ctx context.Context, registry *hub.Registry, logger *zap.Logger) {
	events := registry.Watch(ctx)

	go func() {
		for ev := range events {
			if ev.Kind != hub.EventAttached {
				continue
			}

			reply, err := get(ctx, registry, ev.Peer)
			if err != nil {
				logger.Warn("reach peer failed", zap.String("peer", ev.Peer), zap.Error(err))

				continue
			}

			logger.Info("reached peer through tunnel (token-authenticated)",
				zap.String("peer", ev.Peer), zap.String("reply", reply))
		}
	}()
}

func get(ctx context.Context, registry *hub.Registry, peer string) (string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "http://peer.invalid/hello", nil)
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
