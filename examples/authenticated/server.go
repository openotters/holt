package main

import (
	"context"
	"errors"

	"github.com/openotters/holt"
)

// peerForToken maps a bearer token to the peer identity it proves. A
// real hub validates a JWT signature or an mTLS certificate here.
func peerForToken(_ context.Context, token string) (string, error) {
	if token != "tok-alice" {
		return "", errors.New("unknown token")
	}

	return "alice", nil
}

// startHub is the server half. The one option that matters here:
// WithAuthBearer guards the upgrade with the token check and keys
// each tunnel by the peer id the token proves.
func startHub(ctx context.Context) *holt.Server {
	srv := holt.NewServer(
		holt.WithTunnel(holt.NewTunnel(holt.DefaultTunnelAddr,
			holt.WithAuthBearer(peerForToken),
		)),
		holt.WithProxy(nil),
	)
	go func() { _ = srv.Run(ctx) }()

	return srv
}
