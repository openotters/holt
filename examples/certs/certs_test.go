package certs_test

import (
	"crypto/x509"
	"testing"

	"github.com/openotters/holt/examples/certs"
)

// TestEnsureDirRoundTrip confirms the generated demo PKI is usable
// for mutual TLS: the issued certs load, and the CA pool verifies
// them.
func TestEnsureDirRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	if err := certs.EnsureDir(dir); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	// Idempotent: a second call must not regenerate or fail.
	if err := certs.EnsureDir(dir); err != nil {
		t.Fatalf("ensure again: %v", err)
	}

	pool, err := certs.LoadCA(dir)
	if err != nil {
		t.Fatalf("load ca: %v", err)
	}

	for _, name := range []string{certs.Hub, certs.Peer} {
		cert, loadErr := certs.Load(dir, name)
		if loadErr != nil {
			t.Fatalf("load %s: %v", name, loadErr)
		}

		leaf, parseErr := x509.ParseCertificate(cert.Certificate[0])
		if parseErr != nil {
			t.Fatal(parseErr)
		}

		if _, verifyErr := leaf.Verify(x509.VerifyOptions{
			Roots:     pool,
			KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
		}); verifyErr != nil {
			t.Fatalf("%s cert does not verify against the CA: %v", name, verifyErr)
		}
	}
}

// TestIssueNewIdentity mints a certificate for a name the initial
// generation did not cover, verified by the same CA.
func TestIssueNewIdentity(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := certs.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}

	cert, err := certs.Issue(dir, "carol")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	pool, _ := certs.LoadCA(dir)

	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}

	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:     pool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}); err != nil {
		t.Fatalf("issued cert does not verify: %v", err)
	}

	if leaf.Subject.CommonName != "carol" {
		t.Fatalf("CN = %q, want carol", leaf.Subject.CommonName)
	}
}
