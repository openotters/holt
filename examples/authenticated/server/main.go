// Command server is a standalone holt hub with a real identity: it
// accepts reverse tunnels from peers on one port and reaches those
// peers on another by dialing back THROUGH their tunnels.
//
// Peers authenticate with a bearer token that maps to their identity;
// the hub keys tunnels by that authenticated id, never by anything
// the peer asserts in the handshake. A peer with an unknown token is
// rejected at the upgrade and never lands in the registry.
//
//	go run ./examples/authenticated/server
//
// Then run one or more peers (see ../client) and:
//
//	curl -H 'x-tunnel-peer: alice' localhost:7202/hello      # reach alice through her tunnel
//	curl -H 'x-tunnel-peer: bob'   localhost:7202/time
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"github.com/openotters/holt"
	"github.com/openotters/holt/pkg/registry"
)

// peerForToken maps a demo bearer token to the peer identity it
// proves. A real hub validates a JWT signature or an mTLS certificate
// here. This func is the whole identity seam: WithAuthBearer wires it
// as both the upgrade guard and the registry key.
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
	tunnelAddr := flag.String("addr", "127.0.0.1:7200", "tunnel (WebSocket) listen address for peers")
	proxyAddr := flag.String("proxy", "127.0.0.1:7202", "proxy listen address (reach peers here)")
	flag.Parse()

	if err := run(*tunnelAddr, *proxyAddr); err != nil {
		log.Fatal(err)
	}
}

func run(tunnelAddr, proxyAddr string) error {
	logger, _ := zap.NewDevelopment()
	defer func() { _ = logger.Sync() }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The whole hub: a bearer-guarded tunnel endpoint peers attach to,
	// and a proxy that reaches them. Run binds, serves, and drains on
	// Ctrl-C.
	srv := holt.NewServer(
		holt.WithLogger(logger),
		holt.WithTunnel(holt.NewTunnel(tunnelAddr, holt.WithAuthBearer(peerForToken))),
		holt.WithProxy(holt.NewProxy(proxyAddr,
			holt.WithErrorHook(func(_ context.Context, reason string) {
				logger.Info("proxy miss", zap.String("reason", reason))
			}),
		)),
	)

	logAttachEvents(ctx, srv.Registry(), logger)

	return srv.Run(ctx)
}

// logAttachEvents narrates attach/detach to the log.
func logAttachEvents(ctx context.Context, reg *registry.Registry, logger *zap.Logger) {
	events := reg.Watch(ctx)

	go func() {
		for ev := range events {
			switch ev.Kind {
			case registry.EventAttached:
				logger.Info("peer attached", zap.String("peer", ev.Peer))
			case registry.EventDetached:
				logger.Info("peer detached", zap.String("peer", ev.Peer), zap.String("reason", ev.Reason))
			}
		}
	}()
}
