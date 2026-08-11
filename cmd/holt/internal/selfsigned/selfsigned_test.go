package selfsigned_test

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"slices"
	"testing"

	"github.com/openotters/holt/cmd/holt/internal/selfsigned"
)

func leafFromPEM(t *testing.T, certPEM []byte) *x509.Certificate {
	t.Helper()

	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("no PEM block")
	}

	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}

	return leaf
}

func TestRenewPreservesSecretAndSANsChangesCert(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	first, err := selfsigned.LoadOrCreate(dir, []string{"127.0.0.1", "localhost", "hub.example.com"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	renewed, err := selfsigned.Renew(dir)
	if err != nil {
		t.Fatalf("renew: %v", err)
	}

	if bytes.Equal(first.CertPEM, renewed.CertPEM) {
		t.Fatal("renew did not change the certificate")
	}

	if !bytes.Equal(first.JWTSecret, renewed.JWTSecret) {
		t.Fatal("renew must preserve the JWT secret")
	}

	leaf := leafFromPEM(t, renewed.CertPEM)
	if !slices.Contains(leaf.DNSNames, "hub.example.com") || !slices.Contains(leaf.DNSNames, "localhost") {
		t.Fatalf("renewed cert lost DNS SANs: %v", leaf.DNSNames)
	}

	if len(leaf.IPAddresses) == 0 {
		t.Fatal("renewed cert lost IP SANs")
	}
}

func TestRenewErrorsWithoutExistingMaterial(t *testing.T) {
	t.Parallel()

	if _, err := selfsigned.Renew(t.TempDir()); err == nil {
		t.Fatal("renew on an empty dir should error")
	}
}
