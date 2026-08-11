// Command server is a holt hub that lets you call a peer's gRPC
// service through the tunnel with grpcurl. Peers attach and serve a
// real gRPC service (health + reflection) over their tunnel; the hub
// exposes an operator gRPC endpoint that reverse-proxies each call to
// the peer named in an "x-tunnel-peer" header.
//
//	go run ./examples/grpc-tunnel/server
//	go run ./examples/grpc-tunnel/client --id client-1   # in another terminal
//
// Then, addressing a peer by header:
//
//	grpcurl -plaintext -H 'x-tunnel-peer: client-1' localhost:7501 list
//	grpcurl -plaintext -H 'x-tunnel-peer: client-1' localhost:7501 \
//	        grpc.health.v1.Health/Check
//
// The hub carries the whole gRPC exchange — including reflection —
// down the tunnel to the peer, which has no listener of its own.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/openotters/holt/api/v1/holtv1connect"
	"github.com/openotters/holt/hub"
)

const (
	peerIDHeader   = "x-peer-id"      // peer → hub, on Attach: who am I
	routeHeader    = "x-tunnel-peer"  // operator → hub: which peer to reach
	placeholderURL = "peer.invalid"   // host the tunnel RoundTripper ignores
)

type peerCtxKey struct{}

func main() {
	tunnelAddr := flag.String("addr", "127.0.0.1:7500", "tunnel (gRPC) listen address for peers")
	grpcAddr := flag.String("grpc", "127.0.0.1:7501", "operator gRPC endpoint (call peers via grpcurl)")
	flag.Parse()

	if err := run(*tunnelAddr, *grpcAddr); err != nil {
		log.Fatal(err)
	}
}

func run(tunnelAddr, grpcAddr string) error {
	logger, _ := zap.NewDevelopment()
	defer func() { _ = logger.Sync() }()

	registry := hub.NewRegistry(logger, hub.WithHubID("hub"))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logAttachEvents(ctx, registry, logger)

	tunnelSrv, err := serveTunnels(registry, tunnelAddr, logger)
	if err != nil {
		return err
	}

	grpcSrv, err := serveOperatorGRPC(registry, grpcAddr)
	if err != nil {
		return err
	}

	logger.Info("hub up",
		zap.String("tunnels", tunnelAddr),
		zap.String("operator_grpc", grpcAddr))
	logger.Info("try: grpcurl -plaintext -H 'x-tunnel-peer: client-1' " + grpcAddr + " list")

	<-ctx.Done()
	registry.StopAllTunnels("shutting-down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = tunnelSrv.Shutdown(shutdownCtx)
	_ = grpcSrv.Shutdown(shutdownCtx)

	return nil
}

// serveTunnels stands up Tunnel.Attach; the peer id arrives in the
// x-peer-id header (demo trust — a real hub authenticates it).
func serveTunnels(registry *hub.Registry, addr string, logger *zap.Logger) (*http.Server, error) {
	identity := func(ctx context.Context) (string, error) {
		peer, _ := ctx.Value(peerCtxKey{}).(string)
		if peer == "" {
			return "", errors.New("missing x-peer-id header")
		}

		return peer, nil
	}

	path, handler := holtv1connect.NewTunnelHandler(hub.NewHandler(registry, identity, logger))

	mux := http.NewServeMux()
	mux.Handle(path, peerIDMiddleware(handler))

	return h2cServer(mux, addr)
}

// serveOperatorGRPC exposes an h2c gRPC endpoint that reverse-proxies
// each call to the peer named in the route header, through its tunnel.
func serveOperatorGRPC(registry *hub.Registry, addr string) (*http.Server, error) {
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = placeholderURL
			pr.Out.Host = placeholderURL
			// NB: the route header is read (and stripped) by the
			// transport below — deleting it here would hide it.
		},
		Transport:     peerRouter{registry: registry},
		FlushInterval: -1, // flush every write — required for gRPC streaming
		ErrorHandler:  grpcError,
	}

	return h2cServer(proxy, addr)
}

// peerRouter is the ReverseProxy transport: it reads the route header
// and dispatches the request down that peer's tunnel.
type peerRouter struct {
	registry *hub.Registry
}

func (pr peerRouter) RoundTrip(req *http.Request) (*http.Response, error) {
	peer := req.Header.Get(routeHeader)
	if peer == "" {
		return nil, errors.New("set the " + routeHeader + " header to the target peer id")
	}

	// Strip the routing header so the peer's gRPC service doesn't see it.
	req.Header.Del(routeHeader)

	if !pr.registry.Attached(peer) {
		return nil, fmt.Errorf("peer %q is not attached", peer)
	}

	return pr.registry.RoundTripper(peer).RoundTrip(req)
}

// grpcError renders a proxy error as a clean gRPC UNAVAILABLE so
// grpcurl reports it instead of choking on a non-gRPC body.
func grpcError(w http.ResponseWriter, _ *http.Request, err error) {
	w.Header().Set("Content-Type", "application/grpc")
	w.Header().Set("Grpc-Status", "14") // UNAVAILABLE
	w.Header().Set("Grpc-Message", err.Error())
	w.WriteHeader(http.StatusOK)
}

// peerIDMiddleware lifts the x-peer-id header into the request context
// for the Attach identity func.
func peerIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(peerIDHeader)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), peerCtxKey{}, id)))
	})
}

func logAttachEvents(ctx context.Context, registry *hub.Registry, logger *zap.Logger) {
	events := registry.Watch(ctx)

	go func() {
		for ev := range events {
			switch ev.Kind {
			case hub.EventAttached:
				logger.Info("peer attached (gRPC service reachable)", zap.String("peer", ev.Peer))
			case hub.EventDetached:
				logger.Info("peer detached", zap.String("peer", ev.Peer), zap.String("reason", ev.Reason))
			}
		}
	}()
}

// h2cServer starts an unencrypted-HTTP/2 server on addr (what
// grpc/grpcurl -plaintext expect) and returns it.
func h2cServer(handler http.Handler, addr string) (*http.Server, error) {
	var protocols http.Protocols
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, Protocols: &protocols}

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
