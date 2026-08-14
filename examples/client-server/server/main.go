// Command server is a standalone holt hub. It accepts reverse
// tunnels from peers on one port and exposes an operator HTTP API on
// another that reaches those peers by dialing back THROUGH their
// tunnels — so you can curl a peer that has no listener of its own.
//
//	go run ./examples/client-server/server
//
// Then run one or more peers (see ../client) and:
//
//	curl localhost:7001/peers                 # who is attached
//	curl localhost:7001/peers/alice/hello     # reach alice through her tunnel
//	curl localhost:7001/peers/alice/time
//
// Peers authenticate with a bearer token that maps to their identity;
// the hub keys tunnels by that authenticated id, never by anything the
// peer asserts in the handshake.
package main

import (
	"context"
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

	"github.com/openotters/holt/hub"
)

// tokens maps a demo bearer token to the peer identity it proves. A
// real hub validates a JWT signature or an mTLS certificate here.
var tokens = map[string]string{
	"tok-alice": "alice",
	"tok-bob":   "bob",
}

type peerCtxKey struct{}

func main() {
	tunnelAddr := flag.String("addr", "127.0.0.1:7000", "tunnel (WebSocket) listen address for peers")
	httpAddr := flag.String("http", "127.0.0.1:7001", "operator HTTP API listen address")
	flag.Parse()

	if err := run(*tunnelAddr, *httpAddr); err != nil {
		log.Fatal(err)
	}
}

func run(tunnelAddr, httpAddr string) error {
	logger, _ := zap.NewDevelopment()
	defer func() { _ = logger.Sync() }()

	registry := hub.NewRegistry(logger, hub.WithHubID(hostname()))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logAttachEvents(ctx, registry, logger)

	tunnelSrv, err := serveTunnels(registry, tunnelAddr, logger)
	if err != nil {
		return err
	}

	httpSrv, err := serveOperatorAPI(registry, httpAddr)
	if err != nil {
		return err
	}

	logger.Info("hub up",
		zap.String("tunnels", tunnelAddr),
		zap.String("operator_api", httpAddr))

	<-ctx.Done()
	logger.Info("draining")

	registry.StopAllTunnels("shutting-down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = tunnelSrv.Shutdown(shutdownCtx)
	_ = httpSrv.Shutdown(shutdownCtx)

	return nil
}

// serveTunnels stands up the WebSocket attach handler on a plaintext
// listener, behind bearer-token auth middleware.
func serveTunnels(registry *hub.Registry, addr string, logger *zap.Logger) (*http.Server, error) {
	identity := func(ctx context.Context) (string, error) {
		peer, _ := ctx.Value(peerCtxKey{}).(string)
		if peer == "" {
			return "", errors.New("no authenticated peer")
		}

		return peer, nil
	}

	mux := http.NewServeMux()
	mux.Handle("/", requireBearer(hub.NewHandler(registry, identity, logger)))

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	return listenAndServe(srv, addr)
}

// serveOperatorAPI exposes the peer roster and a reverse proxy that
// reaches each peer through its tunnel.
func serveOperatorAPI(registry *hub.Registry, addr string) (*http.Server, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /peers", listPeers(registry))
	mux.HandleFunc("/peers/{id}/{path...}", proxyToPeer(registry))

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	return listenAndServe(srv, addr)
}

// listPeers reports every live tunnel this hub owns.
func listPeers(registry *hub.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		tunnels := registry.ListTunnels()
		if len(tunnels) == 0 {
			_, _ = io.WriteString(w, "no peers attached\n")

			return
		}

		for _, tunnel := range tunnels {
			_, _ = fmt.Fprintf(w, "%-12s version=%-12s attached=%s\n",
				tunnel.Peer, tunnel.PeerVersion, tunnel.AttachedAt.Format(time.RFC3339))
		}
	}
}

// proxyToPeer forwards /peers/{id}/{path...} through the named peer's
// tunnel — the operator reaches a listenerless peer as if it were a
// normal upstream.
func proxyToPeer(registry *hub.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !registry.Attached(id) {
			http.Error(w, fmt.Sprintf("peer %q not attached", id), http.StatusBadGateway)

			return
		}

		// The host is a placeholder the tunnel RoundTripper ignores;
		// only the path matters, forwarded to the peer's own handler.
		outURL := "http://peer.invalid/" + r.PathValue("path")
		if r.URL.RawQuery != "" {
			outURL += "?" + r.URL.RawQuery
		}

		out, err := http.NewRequestWithContext(r.Context(), r.Method, outURL, r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)

			return
		}

		out.Header = r.Header.Clone()

		client := &http.Client{Transport: registry.RoundTripper(id)}

		resp, err := client.Do(out)
		if err != nil {
			http.Error(w, "tunnel: "+err.Error(), http.StatusBadGateway)

			return
		}
		defer func() { _ = resp.Body.Close() }()

		for k, vs := range resp.Header {
			w.Header()[k] = vs
		}

		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}
}

// logAttachEvents narrates attach/detach to the log.
func logAttachEvents(ctx context.Context, registry *hub.Registry, logger *zap.Logger) {
	events := registry.Watch(ctx)

	go func() {
		for ev := range events {
			switch ev.Kind {
			case hub.EventAttached:
				logger.Info("peer attached", zap.String("peer", ev.Peer))
			case hub.EventDetached:
				logger.Info("peer detached", zap.String("peer", ev.Peer), zap.String("reason", ev.Reason))
			}
		}
	}()
}

// requireBearer validates the bearer token on the WebSocket upgrade
// request and stamps the resolved peer id onto the context.
func requireBearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer, ok := tokens[bearer(r.Header.Get("Authorization"))]
		if !ok {
			http.Error(w, "invalid or missing bearer token", http.StatusUnauthorized)

			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), peerCtxKey{}, peer)))
	})
}

func bearer(h string) string {
	const prefix = "Bearer "
	if len(h) > len(prefix) && h[:len(prefix)] == prefix {
		return h[len(prefix):]
	}

	return ""
}

func listenAndServe(srv *http.Server, addr string) (*http.Server, error) {
	var lc net.ListenConfig

	lis, err := lc.Listen(context.Background(), "tcp", addr)
	if err != nil {
		return nil, err
	}

	go func() {
		if err := srv.Serve(lis); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("serve %s: %v", addr, err)
		}
	}()

	return srv, nil
}

func hostname() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}

	return "hub"
}
