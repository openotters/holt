// Command starter-client is a minimal, self-contained holt peer —
// a template to copy when writing your own client. It joins a hub with
// a token from `holt enroll`, serves one HTTP handler over the
// tunnel, and listens on nothing.
//
// Everything a peer needs is here and nothing else: decode the token,
// then dial.Run your handler (JWT auth rides the WebSocket upgrade;
// the tunnel URL's scheme picks the transport). The join token is
// decoded inline (a tiny base64+JSON struct) so this file has no
// dependency on the holt CLI's internals — copy it and go.
//
//	# on the hub machine:
//	holt hub &
//	holt enroll myservice        # prints a token
//
//	# anywhere:
//	go run ./cmd/starter-client --token <paste>
//
//	# reach it through the hub:
//	curl -H 'x-tunnel-peer: myservice' http://localhost:7002/
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"github.com/openotters/holt/dial"
)

// joinToken mirrors what `holt enroll` prints: the hub's tunnel URL and
// the peer's JWT. Kept inline so this starter has no internal imports.
type joinToken struct {
	Peer      string `json:"peer"`
	TunnelURL string `json:"tunnel_url"`
	JWT       string `json:"jwt"`
}

func main() {
	tok := flag.String("token", "", "join token from `holt enroll` (required)")
	flag.Parse()

	if *tok == "" {
		log.Fatal("--token is required (run `holt enroll <name>` to get one)")
	}

	if err := run(*tok); err != nil {
		log.Fatal(err)
	}
}

func run(rawToken string) error {
	// ── 1. Decode the join token: peer name, tunnel URL, JWT. ───────────
	jt, err := decodeToken(rawToken)
	if err != nil {
		return err
	}

	logger, _ := zap.NewDevelopment()
	defer func() { _ = logger.Sync() }()

	// ── 2. The handler you expose over the tunnel. Replace with yours. ──
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintf(w, "hello from %s — reached through the tunnel\n", jt.Peer)
	})

	// ── 3. Attach and serve until interrupted (auto-redials). ───────────
	//      The tunnel URL comes straight from the token: wss dials a TLS
	//      WebSocket (verified with the system roots, so it works through
	//      a TLS edge like Cloudflare or an ingress); ws dials plaintext.
	//      http and https are accepted as aliases, so tokens minted before
	//      0.13 keep working. The JWT goes out as the Authorization header
	//      of the upgrade request, which is how the hub authenticates the
	//      peer. Transport encryption is the deployment's job — the token
	//      has no cert.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("joined; serving over the tunnel", zap.String("peer", jt.Peer))

	return dial.Run(ctx, dial.Options{
		URL:     jt.TunnelURL,
		Header:  http.Header{"Authorization": {"Bearer " + jt.JWT}},
		Handler: handler,
		Version: "starter-client",
		Logger:  logger,
	})
}

func decodeToken(s string) (joinToken, error) {
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return joinToken{}, fmt.Errorf("token is not valid base64: %w", err)
	}

	var jt joinToken
	if unmarshalErr := json.Unmarshal(raw, &jt); unmarshalErr != nil {
		return joinToken{}, fmt.Errorf("token payload is invalid: %w", unmarshalErr)
	}

	return jt, nil
}
