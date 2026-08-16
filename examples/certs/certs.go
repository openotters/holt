// Package certs is a tiny demo PKI for the separated client/server
// holt examples. It generates a CA plus two leaf certificates —
// one named "hub", one named "peer" — each valid for BOTH server and
// client authentication, and writes them as PEM files that two
// separate processes can load.
//
// A single leaf that works in either TLS role keeps the examples
// simple: the transport-tls hub uses the "hub" cert as its server
// cert while the peer uses "peer" as its client cert; the encrypted
// example (roles inverted, peer is the TLS server) uses the same two
// files the other way round. Both sides trust the shared CA.
//
// This is example scaffolding, not a real PKI: keys live in a temp
// dir, unencrypted. Never reuse it for anything real.
package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

// Names of the two identities issued by EnsureDir.
const (
	Hub  = "hub"
	Peer = "peer"
)

// DefaultDir is the shared location both example binaries default to,
// so the server can generate the certs and the client can read them
// without any flags.
func DefaultDir() string {
	return filepath.Join(os.TempDir(), "holt-example-certs")
}

// EnsureDir generates the CA + hub + peer certificates into dir if
// they are not already present. Idempotent: an existing ca.pem short-
// circuits. Start the server (which calls this) before the client.
func EnsureDir(dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "ca.pem")); err == nil {
		return nil // already generated
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	caCert, caKey, err := newCA()
	if err != nil {
		return err
	}

	if err := writePEM(filepath.Join(dir, "ca.pem"), "CERTIFICATE", caCert.Raw); err != nil {
		return err
	}

	caKeyDER, err := x509.MarshalECPrivateKey(caKey)
	if err != nil {
		return err
	}

	if err := writePEM(filepath.Join(dir, "ca-key.pem"), "EC PRIVATE KEY", caKeyDER); err != nil {
		return err
	}

	for _, name := range []string{Hub, Peer} {
		leaf, key, err := newLeaf(name, caCert, caKey)
		if err != nil {
			return err
		}

		keyDER, err := x509.MarshalECPrivateKey(key)
		if err != nil {
			return err
		}

		if err := writePEM(filepath.Join(dir, name+".pem"), "CERTIFICATE", leaf); err != nil {
			return err
		}

		if err := writePEM(filepath.Join(dir, name+"-key.pem"), "EC PRIVATE KEY", keyDER); err != nil {
			return err
		}
	}

	return nil
}

// LoadCA returns a pool containing the demo CA — the trust anchor for
// verifying the other side's certificate.
func LoadCA(dir string) (*x509.CertPool, error) {
	pemBytes, err := os.ReadFile(filepath.Join(dir, "ca.pem"))
	if err != nil {
		return nil, fmt.Errorf("certs: read CA (run the server first, or share -certs dir): %w", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		return nil, fmt.Errorf("certs: CA PEM in %s is invalid", dir)
	}

	return pool, nil
}

// Load returns the tls.Certificate for the named identity (Hub or Peer).
func Load(dir, name string) (tls.Certificate, error) {
	cert, err := tls.LoadX509KeyPair(
		filepath.Join(dir, name+".pem"),
		filepath.Join(dir, name+"-key.pem"),
	)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("certs: load %s (run the server first?): %w", name, err)
	}

	return cert, nil
}

// Issue mints a fresh leaf certificate for name, signed by the demo
// CA, and returns it in memory (nothing written to disk). It lets a
// client present a certificate carrying its OWN identity — the CN the
// hub then reads as the authenticated peer id. Requires EnsureDir to
// have created ca.pem / ca-key.pem.
func Issue(dir, name string) (tls.Certificate, error) {
	caCert, err := loadCert(filepath.Join(dir, "ca.pem"))
	if err != nil {
		return tls.Certificate{}, err
	}

	caKey, err := loadKey(filepath.Join(dir, "ca-key.pem"))
	if err != nil {
		return tls.Certificate{}, err
	}

	der, key, err := newLeaf(name, caCert, caKey)
	if err != nil {
		return tls.Certificate{}, err
	}

	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return tls.Certificate{}, err
	}

	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, nil
}

func loadCert(path string) (*x509.Certificate, error) {
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("certs: read %s: %w", path, err)
	}

	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("certs: %s is not PEM", path)
	}

	return x509.ParseCertificate(block.Bytes)
}

func loadKey(path string) (*ecdsa.PrivateKey, error) {
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("certs: read %s: %w", path, err)
	}

	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("certs: %s is not PEM", path)
	}

	return x509.ParseECPrivateKey(block.Bytes)
}

func newCA() (*x509.Certificate, *ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}

	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "holt-example-ca"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, err
	}

	return cert, key, nil
}

func newLeaf(name string, ca *x509.Certificate, caKey *ecdsa.PrivateKey) ([]byte, *ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		// Valid in either TLS role so one file serves both examples.
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, err
	}

	return der, key, nil
}

func writePEM(path, blockType string, der []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	return pem.Encode(f, &pem.Block{Type: blockType, Bytes: der})
}
