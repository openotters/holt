package main

import (
	"context"
	"net/http"

	"github.com/openotters/holt"
)

// startPeer is the client half: serve a handler back over the tunnel
// while listening on nothing. In your own program this is the whole
// peer side.
func startPeer(ctx context.Context) {
	handler := http.NewServeMux()
	handler.HandleFunc("/whoami", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("I am the peer; the hub reached me through the tunnel"))
	})

	go func() {
		_ = holt.NewClient("ws://"+holt.DefaultTunnelAddr, handler,
			holt.WithHeader(holt.DevPeerHeader, "peer"),
		).Run(ctx)
	}()
}
