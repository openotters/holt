// Package hubsecret manages the hub's JWT signing secret: a 32-byte
// random value the hub signs peer JWTs with, and enroll (local mode)
// reads to mint tokens. Rotating it invalidates every JWT already
// issued, so peers must be re-enrolled.
//
// Where it lives follows the hub's storage: the jwt-secret file in the
// state directory for a single hub, or the shared SQL backend when one
// is configured (see SQLStore). A fleet pointed at the same database
// therefore shares one signing key without sharing a volume, and
// rotating on any hub rotates for all of them.
//
// This is all the persistent identity the hub needs: transport
// encryption is the deployment's job (a TLS edge, ingress, or mesh), so
// there is no certificate to generate, pin, or renew.
package hubsecret

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
)

const (
	secretFile = "jwt-secret"
	secretLen  = 32
)

// Store is where the signing secret lives.
type Store interface {
	// LoadOrCreate returns the secret, generating and persisting one
	// the first time. Concurrent callers converge on one value.
	LoadOrCreate(ctx context.Context) ([]byte, error)
	// Load returns the existing secret, erroring when there is none.
	Load(ctx context.Context) ([]byte, error)
	// Rotate replaces the secret and returns the new value.
	Rotate(ctx context.Context) ([]byte, error)
	// Describe names the backend for logs and operator messages.
	Describe() string
}

// newSecret is the raw material: 32 bytes from crypto/rand, used as an
// HMAC-SHA256 key (the JWTs are HS256, so this same value signs and
// verifies).
func newSecret() ([]byte, error) {
	secret := make([]byte, secretLen)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("hubsecret: generate: %w", err)
	}

	return secret, nil
}

// FileStore keeps the secret as the jwt-secret file in a state
// directory, 0600 inside a 0700 directory.
type FileStore struct{ dir string }

// NewFile returns the file-backed store for a state directory.
func NewFile(dir string) *FileStore { return &FileStore{dir: dir} }

// Describe names the file for operator messages.
func (f *FileStore) Describe() string { return filepath.Join(f.dir, secretFile) }

// LoadOrCreate returns the secret, generating and persisting one on
// first run. An existing jwt-secret (e.g. from an older hub) is reused
// as-is, so tokens keep their signing key across an upgrade.
func (f *FileStore) LoadOrCreate(_ context.Context) ([]byte, error) {
	if err := os.MkdirAll(f.dir, 0o700); err != nil {
		return nil, err
	}

	secret, err := os.ReadFile(f.path())
	if err == nil {
		return secret, nil
	}

	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("hubsecret: %w", err)
	}

	return f.generate()
}

// Load reads an existing secret, erroring if the hub was never run.
func (f *FileStore) Load(_ context.Context) ([]byte, error) {
	secret, err := os.ReadFile(f.path())
	if err != nil {
		return nil, fmt.Errorf("hubsecret: load (run the hub first?): %w", err)
	}

	return secret, nil
}

// Rotate replaces the secret, invalidating every JWT signed with the
// old value.
func (f *FileStore) Rotate(_ context.Context) ([]byte, error) {
	if err := os.MkdirAll(f.dir, 0o700); err != nil {
		return nil, err
	}

	return f.generate()
}

func (f *FileStore) path() string { return filepath.Join(f.dir, secretFile) }

func (f *FileStore) generate() ([]byte, error) {
	secret, err := newSecret()
	if err != nil {
		return nil, err
	}

	if writeErr := os.WriteFile(f.path(), secret, 0o600); writeErr != nil {
		return nil, fmt.Errorf("hubsecret: write: %w", writeErr)
	}

	return secret, nil
}

// PeekFile returns the secret in dir when the file exists, and nil
// when it does not. It is how a hub moving to a shared backend adopts
// the identity it already had, instead of minting a new one and
// invalidating every token in the field.
func PeekFile(dir string) []byte {
	secret, err := os.ReadFile(filepath.Join(dir, secretFile))
	if err != nil {
		return nil
	}

	return secret
}

var _ Store = (*FileStore)(nil)
