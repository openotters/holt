// Command starter-client is a minimal, self-contained holt peer —
// a template to copy when writing your own client. It joins a hub with
// a token from `holt enroll`, serves one HTTP handler over the
// tunnel, and listens on nothing.
//
// Everything a peer needs is here and nothing else: decode the token,
// dial the hub (TLS pinned + JWT), and dial.Run your handler. The join
// token is decoded inline (a tiny base64+JSON struct) so this file has
// no dependency on the holt CLI's internals — copy it and go.
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
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	"github.com/openotters/holt/dial"
)

// joinToken mirrors what `holt enroll` prints: the hub address, the
// peer's JWT, and the hub's certificate to pin. Kept inline so this
// starter has no internal imports.
type joinToken struct {
	Peer       string `json:"peer"`
	TunnelAddr string `json:"tunnel_addr"`
	JWT        string `json:"jwt"`
	CAPEM      []byte `json:"ca_pem"`
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
	jt, err := decodeToken(rawToken)
	if err != nil {
		return err
	}

	logger, _ := zap.NewDevelopment()
	defer func() { _ = logger.Sync() }()

	// ── 1. Pin the hub's certificate (encrypt + authenticate the hub). ──
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(jt.CAPEM) {
		return fmt.Errorf("token carries an invalid hub certificate")
	}

	serverName := jt.TunnelAddr
	if host, _, splitErr := net.SplitHostPort(jt.TunnelAddr); splitErr == nil {
		serverName = host
	}

	creds := credentials.NewTLS(&tls.Config{
		RootCAs:    pool,
		ServerName: serverName,
		MinVersion: tls.VersionTLS13,
	})

	// ── 2. Dial the hub, presenting the JWT on every call. ──────────────
	cc, err := grpc.NewClient(jt.TunnelAddr,
		grpc.WithTransportCredentials(creds),
		grpc.WithUnaryInterceptor(bearer(jt.JWT)),
		grpc.WithStreamInterceptor(bearerStream(jt.JWT)))
	if err != nil {
		return err
	}
	defer func() { _ = cc.Close() }()

	// ── 3. The handler you expose over the tunnel. Replace with yours. ──
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintf(w, "hello from %s — reached through the tunnel\n", jt.Peer)
	})

	// ── 4. Attach and serve until interrupted (auto-redials). ───────────
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("joined; serving over the tunnel", zap.String("peer", jt.Peer))

	return dial.Run(ctx, dial.Options{
		Conn:    cc,
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

func bearer(jwt string) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context, method string, req, reply any,
		cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption,
	) error {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+jwt)

		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

func bearerStream(jwt string) grpc.StreamClientInterceptor {
	return func(
		ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn,
		method string, streamer grpc.Streamer, opts ...grpc.CallOption,
	) (grpc.ClientStream, error) {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+jwt)

		return streamer(ctx, desc, cc, method, opts...)
	}
}
