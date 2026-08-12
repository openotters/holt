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

// Ensure loads the hub material, creating it on first run and
// regenerating the certificate when it does not yet cover hosts — e.g.
// a hub whose cert was minted for loopback only, then started with
// --advertise-addr pointing at a public name/IP. Peers pin the cert and
// verify the TLS hostname against the advertised address, so a cert
// missing that SAN makes every join fail the handshake.
//
// Regeneration keeps the JWT secret (only the pinned identity rotates)
// and takes the UNION of the existing SANs and hosts, so loopback keeps
// working. It invalidates tokens that pinned the old cert, hence the
// bool result: true means peers must be re-enrolled.
func Ensure(dir string, hosts []string) (*Material, bool, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, false, err
	}

	if _, err := os.Stat(filepath.Join(dir, certFile)); err != nil {
		// First run: create with the requested SANs, not a "regeneration".
		mat, createErr := create(dir, hosts)

		return mat, false, createErr
	}

	mat, err := Load(dir)
	if err != nil {
		return nil, false, err
	}

	if len(missingSANs(mat.CertPEM, hosts)) == 0 {
		return mat, false, nil
	}

	union := dedupe(append(sansFromPEM(mat.CertPEM), hosts...))

	certPEM, keyPEM, err := generateCert(union)
	if err != nil {
		return nil, false, err
	}

	if err = writeFiles(dir, certPEM, keyPEM, mat.JWTSecret); err != nil {
		return nil, false, err
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, false, err
	}

	return &Material{Cert: cert, CertPEM: certPEM, JWTSecret: mat.JWTSecret}, true, nil
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
	certPEM, keyPEM, err := generateCert(hosts)
	if err != nil {
		return nil, err
	}

	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return nil, err
	}

	if err = writeFiles(dir, certPEM, keyPEM, secret); err != nil {
		return nil, err
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}

	return &Material{Cert: cert, CertPEM: certPEM, JWTSecret: secret}, nil
}

// Renew regenerates the hub's TLS certificate and key in place,
// preserving the existing certificate's SANs and the JWT secret. Every
// enroll token already handed out pins the OLD certificate, so they
// stop working after a renew: peers must be re-enrolled. Errors if no
// material exists yet (run the hub once first).
func Renew(dir string) (*Material, error) {
	old, err := Load(dir)
	if err != nil {
		return nil, err
	}

	hosts := sansFromPEM(old.CertPEM)
	if len(hosts) == 0 {
		hosts = []string{"127.0.0.1", "localhost"}
	}

	certPEM, keyPEM, err := generateCert(hosts)
	if err != nil {
		return nil, err
	}

	// The secret is preserved: renewing rotates the pinned identity,
	// not the JWT signing key.
	if err = writeFiles(dir, certPEM, keyPEM, old.JWTSecret); err != nil {
		return nil, err
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}

	return &Material{Cert: cert, CertPEM: certPEM, JWTSecret: old.JWTSecret}, nil
}

// generateCert produces a fresh self-signed cert + key PEM for hosts.
func generateCert(hosts []string) ([]byte, []byte, error) {
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
		return nil, nil, err
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return certPEM, keyPEM, nil
}

// writeFiles persists the cert, key, and secret with owner-only perms.
func writeFiles(dir string, certPEM, keyPEM, secret []byte) error {
	for _, f := range []struct {
		name string
		data []byte
	}{
		{certFile, certPEM},
		{keyFile, keyPEM},
		{secretFile, secret},
	} {
		if err := os.WriteFile(filepath.Join(dir, f.name), f.data, 0o600); err != nil {
			return err
		}
	}

	return nil
}

// sansFromPEM extracts the IP and DNS SANs from a leaf certificate PEM.
func sansFromPEM(certPEM []byte) []string {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil
	}

	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil
	}

	hosts := make([]string, 0, len(leaf.IPAddresses)+len(leaf.DNSNames))
	for _, ip := range leaf.IPAddresses {
		hosts = append(hosts, ip.String())
	}

	return append(hosts, leaf.DNSNames...)
}

// missingSANs returns the hosts a leaf certificate does not already
// cover (empty ones are ignored). VerifyHostname checks both DNS and IP
// SANs, so it works whether a host is a name or an address.
func missingSANs(certPEM []byte, hosts []string) []string {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return hosts
	}

	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return hosts
	}

	var missing []string
	for _, h := range hosts {
		if h == "" {
			continue
		}

		if leaf.VerifyHostname(h) != nil {
			missing = append(missing, h)
		}
	}

	return missing
}

// dedupe returns the input with empties and duplicates removed, order
// preserved.
func dedupe(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))

	for _, s := range in {
		if s == "" {
			continue
		}

		if _, ok := seen[s]; ok {
			continue
		}

		seen[s] = struct{}{}
		out = append(out, s)
	}

	return out
}
