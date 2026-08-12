package selfsigned_test

import (
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/openotters/holt/cmd/holt/internal/selfsigned"
)

// leaf parses a hub's cert PEM back to a certificate for SAN assertions.
func leaf(t *testing.T, mat *selfsigned.Material) *x509.Certificate {
	t.Helper()

	block, _ := pem.Decode(mat.CertPEM)
	if block == nil {
		t.Fatal("cert PEM did not decode")
	}

	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}

	return c
}

func TestEnsureFirstRunHasAdvertisedSAN(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	mat, regenerated, err := selfsigned.Ensure(dir, []string{"127.0.0.1", "localhost", "holt.example.com"})
	if err != nil {
		t.Fatal(err)
	}

	if regenerated {
		t.Fatal("first run should not report a regeneration")
	}

	// A peer that pins this cert and dials the advertised host must pass
	// TLS name verification.
	if err := leaf(t, mat).VerifyHostname("holt.example.com"); err != nil {
		t.Fatalf("cert should cover the advertised host: %v", err)
	}
}

func TestEnsureAddsSANToLoopbackOnlyCert(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Simulate the pre-fix prod state: a cert minted for loopback only.
	first, _, err := selfsigned.Ensure(dir, []string{"127.0.0.1", "localhost"})
	if err != nil {
		t.Fatal(err)
	}

	if leaf(t, first).VerifyHostname("holt.example.com") == nil {
		t.Fatal("precondition: loopback cert should NOT cover the public host")
	}

	// Restart with --advertise-addr set: the cert must be regenerated to
	// cover the public host, keeping the same JWT secret.
	second, regenerated, err := selfsigned.Ensure(dir, []string{"127.0.0.1", "localhost", "holt.example.com"})
	if err != nil {
		t.Fatal(err)
	}

	if !regenerated {
		t.Fatal("adding a new SAN should report a regeneration")
	}

	if err := leaf(t, second).VerifyHostname("holt.example.com"); err != nil {
		t.Fatalf("regenerated cert should cover the public host: %v", err)
	}

	// Loopback stays covered (union of old + new SANs).
	if err := leaf(t, second).VerifyHostname("127.0.0.1"); err != nil {
		t.Fatalf("regenerated cert dropped loopback: %v", err)
	}

	// The JWT secret is preserved: only the pinned identity rotates, so
	// already-signed JWTs still verify after the cert swap.
	if string(second.JWTSecret) != string(first.JWTSecret) {
		t.Fatal("JWT secret must be preserved across a SAN regeneration")
	}
}

func TestEnsureNoChangeWhenCovered(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	hosts := []string{"127.0.0.1", "localhost", "holt.example.com"}

	if _, _, err := selfsigned.Ensure(dir, hosts); err != nil {
		t.Fatal(err)
	}

	// A restart with the same (already covered) hosts must not rotate the
	// cert, or it would needlessly invalidate live tokens on every boot.
	_, regenerated, err := selfsigned.Ensure(dir, hosts)
	if err != nil {
		t.Fatal(err)
	}

	if regenerated {
		t.Fatal("an already-covered host set must not regenerate the cert")
	}
}

func TestEnsureCoversAdvertisedIP(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// A MetalLB LoadBalancer IP as the advertised address.
	mat, _, err := selfsigned.Ensure(dir, []string{"127.0.0.1", "localhost", "192.168.8.193"})
	if err != nil {
		t.Fatal(err)
	}

	if err := leaf(t, mat).VerifyHostname("192.168.8.193"); err != nil {
		t.Fatalf("cert should cover the advertised IP: %v", err)
	}
}
