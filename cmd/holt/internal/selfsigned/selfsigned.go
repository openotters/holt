// Package selfsigned generates and persists the hub's self-signed TLS
// certificate — used to encrypt the tunnel transport — and its JWT
// secret, as files in the config folder. Clients pin the cert (its PEM
// travels in the enroll token), so the self-signed cert both encrypts
// the channel and authenticates the hub.
package selfsigned

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
	"net"
	"os"
	"path/filepath"
	"time"
)

const (
	certFile   = "hub-cert.pem"
	keyFile    = "hub-key.pem"
	secretFile = "jwt-secret"
)

// Material is a hub's persistent identity: its TLS cert (+key) and the
// HMAC secret it signs peer JWTs with.
type Material struct {
	Cert      tls.Certificate
	CertPEM   []byte // the leaf cert, for clients to pin
	JWTSecret []byte
}

// LoadOrCreate reads the hub material from dir, generating and
// persisting it on first run. hosts are the SANs to put on the cert.
func LoadOrCreate(dir string, hosts []string) (*Material, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}

	if _, err := os.Stat(filepath.Join(dir, certFile)); err == nil {
		return Load(dir)
	}

	return create(dir, hosts)
}

// Load reads existing hub material from dir, erroring if it was never
// created. Used by `enroll` to mint against the SAME cert + secret a
// running hub uses.
func Load(dir string) (*Material, error) {
	cert, err := tls.LoadX509KeyPair(filepath.Join(dir, certFile), filepath.Join(dir, keyFile))
	if err != nil {
		return nil, fmt.Errorf("selfsigned: load cert (run the hub first?): %w", err)
	}

	certPEM, err := os.ReadFile(filepath.Join(dir, certFile))
	if err != nil {
		return nil, err
	}

	secret, err := os.ReadFile(filepath.Join(dir, secretFile))
	if err != nil {
		return nil, err
	}

	return &Material{Cert: cert, CertPEM: certPEM, JWTSecret: secret}, nil
}

func create(dir string, hosts []string) (*Material, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "holt-hub"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:         true,
	}

	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return nil, err
	}

	for _, f := range []struct {
		name string
		data []byte
	}{
		{certFile, certPEM},
		{keyFile, keyPEM},
		{secretFile, secret},
	} {
		if err = os.WriteFile(filepath.Join(dir, f.name), f.data, 0o600); err != nil {
			return nil, err
		}
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}

	return &Material{Cert: cert, CertPEM: certPEM, JWTSecret: secret}, nil
}
