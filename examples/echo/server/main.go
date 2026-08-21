// Command server is the hub half of the echo example, with zero
// configuration: holt.NewServer() serves the tunnel on 127.0.0.1:7200
// and a proxy on :7202.
//
// No identity is configured, so the development identity applies:
// peers name themselves with the x-holt-peer header, nothing verifies
// the claim, and the tunnel refuses to bind anywhere another machine
// could reach. Loopback demos only — see the `authenticated` example
// for a real identity.
//
//	go run ./examples/echo/server
//
// Then run the peer (see ../client) and reach it through the proxy:
//
//	curl -H 'x-tunnel-peer: peer' http://127.0.0.1:7202/whoami
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"github.com/openotters/holt"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger, _ := zap.NewDevelopment()
	defer func() { _ = logger.Sync() }()

	fmt.Printf("tunnel on ws://%s — proxy on http://%s\n", holt.DefaultTunnelAddr, holt.DefaultProxyAddr)
	fmt.Println("reach the peer:  curl -H 'x-tunnel-peer: peer' http://" + holt.DefaultProxyAddr + "/whoami")

	if err := holt.NewServer(holt.WithLogger(logger)).Run(ctx); err != nil {
		log.Fatal(err)
	}
}
