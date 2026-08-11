package holt_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"testing"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/openotters/holt/api/v1/holtv1connect"
	"github.com/openotters/holt/dial"
	"github.com/openotters/holt/hub"
)

// TestEncryptedTunnel proves payload TLS INSIDE the tunnel: the peer
// serves HTTPS over the tunnel with a self-signed cert, and the hub
// reaches it only when it pins that exact cert. A rogue cert is
// rejected at the inner TLS handshake, so an unpinned peer is never
// reachable.
func TestEncryptedTunnel(t *testing.T) {
	t.Parallel()

	cert, pool := testCert(t, "peer")

	hubTLS := &tls.Config{RootCAs: pool, ServerName: "peer", MinVersion: tls.VersionTLS13}
	peerTLS := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}

	t.Run("pinned cert reaches the peer", func(t *testing.T) {
		t.Parallel()

		registry := runHubAndPeer(t, hubTLS, peerTLS, okHandler())
		waitAttached(t, registry, "peer")

		body, err := tlsGet(t, registry, "peer")
		if err != nil {
			t.Fatalf("get through encrypted tunnel: %v", err)
		}
		if body != "secret ok" {
			t.Fatalf("body = %q", body)
		}
	})

	t.Run("unpinned cert is rejected", func(t *testing.T) {
		t.Parallel()

		rogueCert, _ := testCert(t, "rogue")
		rogueTLS := &tls.Config{Certificates: []tls.Certificate{rogueCert}, MinVersion: tls.VersionTLS13}

		registry := runHubAndPeer(t, hubTLS, rogueTLS, okHandler())
		assertUnreachable(t, registry, "rogue peer was reachable through the tunnel")
	})
}

// TestMutualTLSTunnel proves cert auth in BOTH directions: the peer
// verifies the hub's client cert too. A hub that presents no client
// cert is rejected by the peer's RequireAndVerifyClientCert.
func TestMutualTLSTunnel(t *testing.T) {
	t.Parallel()

	peerCert, peerPool := testCertUsage(t, "peer", x509.ExtKeyUsageServerAuth)
	hubCert, hubPool := testCertUsage(t, "hub", x509.ExtKeyUsageClientAuth)

	hubTLS := &tls.Config{
		RootCAs:      peerPool,
		ServerName:   "peer",
		Certificates: []tls.Certificate{hubCert},
		MinVersion:   tls.VersionTLS13,
	}
	peerTLS := &tls.Config{
		Certificates: []tls.Certificate{peerCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    hubPool,
		MinVersion:   tls.VersionTLS13,
	}

	t.Run("both certs valid", func(t *testing.T) {
		t.Parallel()

		registry := runHubAndPeer(t, hubTLS, peerTLS, okHandler())
		waitAttached(t, registry, "peer")

		body, err := tlsGet(t, registry, "peer")
		if err != nil || body != "secret ok" {
			t.Fatalf("mutual-TLS get: body=%q err=%v", body, err)
		}
	})

	t.Run("hub without client cert is rejected", func(t *testing.T) {
		t.Parallel()

		noCertHub := &tls.Config{RootCAs: peerPool, ServerName: "peer", MinVersion: tls.VersionTLS13}
		registry := runHubAndPeer(t, noCertHub, peerTLS, okHandler())
		assertUnreachable(t, registry, "hub without a client cert was allowed through")
	})
}

// assertUnreachable fails if the peer becomes reachable within 2s.
func assertUnreachable(t *testing.T, registry *hub.Registry, msg string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := tlsGet(t, registry, "peer"); err != nil {
			return // rejected, as required
		}

		time.Sleep(50 * time.Millisecond)
	}

	t.Fatal(msg)
}

// runHubAndPeer wires an encrypted hub + peer and returns the hub's
// registry. The peer serves handler over TLS; the hub pins hubTLS.
func runHubAndPeer(t *testing.T, hubTLS, peerTLS *tls.Config, handler http.Handler) *hub.Registry {
	t.Helper()

	logger := zap.NewNop()
	registry := hub.NewRegistry(logger)
	identity := func(context.Context) (string, error) { return "peer", nil }

	path, h := holtv1connect.NewTunnelHandler(
		hub.NewHandler(registry, identity, logger, hub.WithPeerTLS(hubTLS)),
	)

	mux := http.NewServeMux()
	mux.Handle(path, h)

	var lc net.ListenConfig

	lis, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	var protocols http.Protocols
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, Protocols: &protocols}
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() { _ = srv.Close() })

	cc, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cc.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() {
		_ = dial.Run(ctx, dial.Options{Conn: cc, Handler: handler, TLSConfig: peerTLS, Version: "test", Logger: logger})
	}()

	return registry
}

func okHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/secret", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("secret ok"))
	})

	return mux
}

func tlsGet(t *testing.T, r *hub.Registry, peer string) (string, error) {
	t.Helper()

	client := &http.Client{Transport: r.RoundTripper(peer), Timeout: 2 * time.Second}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://peer.invalid/secret", nil)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	return string(body), nil
}

func testCert(t *testing.T, cn string) (tls.Certificate, *x509.CertPool) {
	t.Helper()

	return testCertUsage(t, cn, x509.ExtKeyUsageServerAuth)
}

func testCertUsage(t *testing.T, cn string, usages ...x509.ExtKeyUsage) (tls.Certificate, *x509.CertPool) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     []string{cn},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  usages,
		IsCA:         true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}

	cert, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
	)
	if err != nil {
		t.Fatal(err)
	}

	cert.Leaf, err = x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}

	pool := x509.NewCertPool()
	pool.AddCert(cert.Leaf)

	return cert, pool
}
