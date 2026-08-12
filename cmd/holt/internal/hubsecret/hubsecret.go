// Package hubsecret manages the hub's JWT signing secret: a 32-byte
// random value persisted as the jwt-secret file in the hub state
// directory. The hub signs peer JWTs with it and enroll (local mode)
// reads it to mint tokens. Rotating it invalidates every JWT already
// issued, so peers must be re-enrolled.
//
// This is all the persistent identity the hub needs: transport
// encryption is the deployment's job (a TLS edge, ingress, or mesh), so
// there is no certificate to generate, pin, or renew.
package hubsecret

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
)

const (
	secretFile = "jwt-secret"
	secretLen  = 32
)

// LoadOrCreate returns the hub's JWT secret, generating and persisting
// one on first run. An existing jwt-secret (e.g. from an older hub) is
// reused as-is, so tokens keep their signing key across an upgrade.
func LoadOrCreate(dir string) ([]byte, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}

	secret, err := os.ReadFile(filepath.Join(dir, secretFile))
	if err == nil {
		return secret, nil
	}

	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("hubsecret: %w", err)
	}

	return generate(dir)
}

// Load reads an existing JWT secret, erroring if the hub was never run.
// Used by `enroll` (local mode) to mint against the SAME secret a
// running hub verifies with.
func Load(dir string) ([]byte, error) {
	secret, err := os.ReadFile(filepath.Join(dir, secretFile))
	if err != nil {
		return nil, fmt.Errorf("hubsecret: load (run the hub first?): %w", err)
	}

	return secret, nil
}

// Rotate replaces the JWT secret with a fresh one, invalidating every
// JWT signed with the old value. Peers must be re-enrolled.
func Rotate(dir string) ([]byte, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}

	return generate(dir)
}

func generate(dir string) ([]byte, error) {
	secret := make([]byte, secretLen)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}

	if err := os.WriteFile(filepath.Join(dir, secretFile), secret, 0o600); err != nil {
		return nil, fmt.Errorf("hubsecret: write: %w", err)
	}

	return secret, nil
}
