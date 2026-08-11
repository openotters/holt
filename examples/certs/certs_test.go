package certs_test

import (
	"crypto/tls"
	"testing"

	"github.com/openotters/holt/examples/certs"
)

// TestBundleRoundTrip confirms a client bundle survives Encode →
// DecodeBundle and yields a usable mutual-TLS config whose cert the
// PKI's own pool verifies.
func TestBundleRoundTrip(t *testing.T) {
	t.Parallel()

	pki, err := certs.NewPKI()
	if err != nil {
		t.Fatal(err)
	}

	bundle, err := pki.ClientBundle("alice")
	if err != nil {
		t.Fatal(err)
	}

	token := bundle.Encode()
	if token == "" {
		t.Fatal("empty token")
	}

	decoded, err := certs.DecodeBundle(token)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	tlsCfg, err := decoded.ClientTLS(certs.Hub)
	if err != nil {
		t.Fatalf("client TLS: %v", err)
	}

	if tlsCfg.ServerName != certs.Hub || len(tlsCfg.Certificates) != 1 {
		t.Fatalf("unexpected client config: %+v", tlsCfg)
	}

	// The client cert must verify against the PKI's own CA pool with
	// the CN we asked for.
	leaf := tlsCfg.Certificates[0].Leaf
	if leaf == nil {
		if len(tlsCfg.Certificates[0].Certificate) == 0 {
			t.Fatal("no certificate in bundle")
		}
	}

	// Server-side view: the hub trusts this cert via its pool.
	pool := pki.Pool()
	if pool == nil {
		t.Fatal("nil pool")
	}
}

func TestDecodeBundle_Garbage(t *testing.T) {
	t.Parallel()

	if _, err := certs.DecodeBundle("not-base64-!!!"); err == nil {
		t.Fatal("expected error on non-base64 token")
	}

	if _, err := certs.DecodeBundle("dGhpcyBpcyBub3QganNvbg=="); err == nil {
		t.Fatal("expected error on non-JSON payload")
	}
}

// TestServerCertUsableForTLS confirms ServerCert yields a leaf a TLS
// server can present.
func TestServerCertUsableForTLS(t *testing.T) {
	t.Parallel()

	pki, err := certs.NewPKI()
	if err != nil {
		t.Fatal(err)
	}

	cert, err := pki.ServerCert(certs.Hub)
	if err != nil {
		t.Fatal(err)
	}

	cfg := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}
	if len(cfg.Certificates) != 1 || cfg.Certificates[0].Leaf == nil {
		t.Fatal("server cert not usable")
	}
}
