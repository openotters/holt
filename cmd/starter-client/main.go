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
	"strings"
	"syscall"

	"go.uber.org/zap"

	"github.com/openotters/holt/dial"
)

// joinToken is what `holt enroll` prints: a JWT in compact
// serialization (header.payload.signature). Its subject is the peer
// name and its audience is the hub's tunnel URL, so the token needs
// no envelope, and the token itself is the credential the peer
// presents. Decoded inline so this starter has no internal imports.
type joinToken struct {
	Peer      string // JWT "sub"
	TunnelURL string // JWT "aud"
	JWT       string // the token itself
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

// decodeToken reads the JWT's claims WITHOUT verifying the signature:
// the peer holds no key, and the hub is the one that checks it on
// attach. Only the middle segment (the payload) is needed.
func decodeToken(s string) (joinToken, error) {
	parts := strings.Split(strings.TrimSpace(s), ".")
	if len(parts) != 3 {
		return joinToken{}, fmt.Errorf("token is not a JWT (want header.payload.signature)")
	}

	// JWT segments are base64url without padding.
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return joinToken{}, fmt.Errorf("token payload is not valid base64url: %w", err)
	}

	var claims struct {
		Subject  string `json:"sub"`
		Audience any    `json:"aud"` // a string, or an array of them
	}
	if unmarshalErr := json.Unmarshal(payload, &claims); unmarshalErr != nil {
		return joinToken{}, fmt.Errorf("token payload is invalid: %w", unmarshalErr)
	}

	jt := joinToken{Peer: claims.Subject, TunnelURL: audience(claims.Audience), JWT: strings.TrimSpace(s)}
	if jt.Peer == "" || jt.TunnelURL == "" {
		return joinToken{}, fmt.Errorf("token is missing its subject or audience; re-enroll")
	}

	return jt, nil
}

// audience reads the "aud" claim, which JWT allows to be a single
// string or an array of them.
func audience(v any) string {
	switch aud := v.(type) {
	case string:
		return aud
	case []any:
		if len(aud) > 0 {
			s, _ := aud[0].(string)

			return s
		}
	}

	return ""
}
