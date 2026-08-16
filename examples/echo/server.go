package main

import (
	"context"

	"github.com/openotters/holt"
)

// startHub is the server half: one call, zero configuration — the
// tunnel endpoint on 127.0.0.1:7000 and a proxy on :7002. In your own
// program this is the whole hub side.
func startHub(ctx context.Context) *holt.Server {
	srv := holt.NewServer()
	go func() { _ = srv.Run(ctx) }()

	return srv
}
