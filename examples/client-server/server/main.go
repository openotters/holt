// Command server is a standalone holt hub. It accepts reverse
// tunnels from peers on one port and reaches those peers on another
// by dialing back THROUGH their tunnels — so you can curl a peer that
// has no listener of its own.
//
//	go run ./examples/client-server/server
//
// Then run one or more peers (see ../client) and:
//
//	curl localhost:7001/peers                                # who is attached
//	curl -H 'x-tunnel-peer: alice' localhost:7002/hello      # reach alice through her tunnel
//	curl -H 'x-tunnel-peer: alice' localhost:7002/time
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

	"github.com/openotters/holt"
)

// peerForToken maps a demo bearer token to the peer identity it
// proves. A real hub validates a JWT signature or an mTLS certificate
// here.
func peerForToken(_ context.Context, token string) (string, error) {
	peers := map[string]string{
		"tok-alice": "alice",
		"tok-bob":   "bob",
	}

	peer, ok := peers[token]
	if !ok {
		return "", errors.New("unknown token")
	}

	return peer, nil
}

func main() {
	tunnelAddr := flag.String("addr", "127.0.0.1:7000", "tunnel (WebSocket) listen address for peers")
	rosterAddr := flag.String("http", "127.0.0.1:7001", "roster HTTP listen address")
	proxyAddr := flag.String("proxy", "127.0.0.1:7002", "proxy listen address (reach peers here)")
	flag.Parse()

	if err := run(*tunnelAddr, *rosterAddr, *proxyAddr); err != nil {
		log.Fatal(err)
	}
}

func run(tunnelAddr, rosterAddr, proxyAddr string) error {
	logger, _ := zap.NewDevelopment()
	defer func() { _ = logger.Sync() }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The whole hub: a bearer-guarded tunnel endpoint peers attach to,
	// and a proxy that reaches them. Run binds, serves, and drains on
	// Ctrl-C.
	srv := holt.NewServer(
		holt.WithLogger(logger),
		holt.WithTunnel(holt.NewTunnel(tunnelAddr,
			holt.WithAuthBearer(peerForToken),
		)),
		holt.WithProxy(holt.NewProxy(proxyAddr,
			holt.WithErrorHook(func(_ context.Context, reason string) {
				logger.Info("proxy miss", zap.String("reason", reason))
			}),
		)),
	)

	logAttachEvents(ctx, srv.Registry(), logger)

	// A tiny operator extra the facade does not own: the peer roster,
	// read straight off the registry.
	rosterSrv, err := serveRoster(srv.Registry(), rosterAddr)
	if err != nil {
		return err
	}

	defer func() { _ = rosterSrv.Close() }()

	return srv.Run(ctx)
}

// serveRoster exposes GET /peers: every live tunnel this hub owns.
func serveRoster(registry *holt.Registry, addr string) (*http.Server, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /peers", func(w http.ResponseWriter, _ *http.Request) {
		tunnels := registry.ListTunnels()
		if len(tunnels) == 0 {
			_, _ = io.WriteString(w, "no peers attached\n")

			return
		}

		for _, tunnel := range tunnels {
			_, _ = fmt.Fprintf(w, "%-12s version=%-12s attached=%s\n",
				tunnel.Peer, tunnel.PeerVersion, tunnel.AttachedAt.Format(time.RFC3339))
		}
	})

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	var lc net.ListenConfig

	lis, err := lc.Listen(context.Background(), "tcp", addr)
	if err != nil {
		return nil, err
	}

	go func() { _ = srv.Serve(lis) }()

	return srv, nil
}

// logAttachEvents narrates attach/detach to the log.
func logAttachEvents(ctx context.Context, registry *holt.Registry, logger *zap.Logger) {
	events := registry.Watch(ctx)

	go func() {
		for ev := range events {
			switch ev.Kind {
			case holt.EventAttached:
				logger.Info("peer attached", zap.String("peer", ev.Peer))
			case holt.EventDetached:
				logger.Info("peer detached", zap.String("peer", ev.Peer), zap.String("reason", ev.Reason))
			}
		}
	}()
}
