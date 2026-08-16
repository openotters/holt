package hubsecret_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/openotters/holt/cmd/holt/internal/hubsecret"
)

func TestLoadOrCreateIsStable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	first, err := hubsecret.NewFile(dir).LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(first) == 0 {
		t.Fatal("secret should not be empty")
	}

	// A second call reuses the persisted secret, so already-issued JWTs
	// keep verifying across restarts.
	second, err := hubsecret.NewFile(dir).LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(first, second) {
		t.Fatal("LoadOrCreate must return the same secret on the second call")
	}
}

func TestLoadErrorsWithoutSecret(t *testing.T) {
	t.Parallel()

	if _, err := hubsecret.NewFile(t.TempDir()).Load(context.Background()); err == nil {
		t.Fatal("Load should error when the hub was never run")
	}
}

func TestRotateChangesSecret(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	before, err := hubsecret.NewFile(dir).LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	after, err := hubsecret.NewFile(dir).Rotate(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Equal(before, after) {
		t.Fatal("Rotate must produce a different secret (invalidating old tokens)")
	}

	// The rotated secret is what a subsequent load sees.
	reloaded, err := hubsecret.NewFile(dir).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(after, reloaded) {
		t.Fatal("Rotate must persist the new secret")
	}
}
