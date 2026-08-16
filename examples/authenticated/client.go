package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/openotters/holt"
)

// startPeer is the client half: dial the hub with a bearer token on
// the upgrade request and serve a name-greeting handler over the
// tunnel.
func startPeer(ctx context.Context, token, name string) {
	handler := http.NewServeMux()
	handler.HandleFunc("/hello", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "hello from %s", name)
	})

	var opts []holt.ClientOption
	if token != "" {
		opts = append(opts, holt.WithBearerToken(token))
	}

	go func() {
		_ = holt.NewClient("ws://"+holt.DefaultTunnelAddr, handler, opts...).Run(ctx)
	}()
}
