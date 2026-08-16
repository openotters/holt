//nolint:testpackage // adoptRotatedSecret is unexported; white-box unit test on purpose.
package commands

import (
	"bytes"
	"context"
	"database/sql"
	"testing"

	"go.uber.org/zap"
	_ "modernc.org/sqlite"

	"github.com/openotters/holt/cmd/holt/internal/hubsecret"
	"github.com/openotters/holt/pkg/jwtauth"
)

// stubTunnels records what the rotation did to the live tunnels.
type stubTunnels struct {
	count      int
	stopped    bool
	stopReason string
}

func (s *stubTunnels) CountTunnels() int { return s.count }
func (s *stubTunnels) StopAllTunnels(reason string) {
	s.stopped = true
	s.stopReason = reason
}

func sharedStore(t *testing.T) *hubsecret.SQLStore {
	t.Helper()

	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}

	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	store := hubsecret.NewSQL(db, hubsecret.SQLite)
	if migErr := store.Migrate(context.Background()); migErr != nil {
		t.Fatal(migErr)
	}

	return store
}

// A rotation performed on another hub has to reach this one: without
// it, this hub keeps verifying with the old secret, so tokens the
// operator was told are dead still work here.
func TestAdoptRotatedSecret(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := sharedStore(t)

	original, err := store.LoadOrCreate(ctx)
	if err != nil {
		t.Fatal(err)
	}

	live := jwtauth.NewSecret(original)
	tunnels := &stubTunnels{count: 4}

	// Nothing has changed yet: the poll must leave the hub alone.
	adoptRotatedSecret(ctx, store, live, tunnels, zap.NewNop())

	if tunnels.stopped {
		t.Fatal("tunnels were closed without a rotation")
	}

	// Another hub rotates.
	rotated, err := store.Rotate(ctx)
	if err != nil {
		t.Fatal(err)
	}

	adoptRotatedSecret(ctx, store, live, tunnels, zap.NewNop())

	if !bytes.Equal(live.Get(), rotated) {
		t.Fatal("the hub kept the old secret, so revoked tokens still verify here")
	}

	if !tunnels.stopped {
		t.Fatal("tunnels authenticated with the revoked secret were left open")
	}

	if tunnels.stopReason == "" {
		t.Fatal("tunnels must be closed with a reason, so peers know not to redial")
	}
}

// A backend that cannot be read is not a reason to distrust the secret
// the hub already holds; a blip must not close every tunnel.
func TestAdoptRotatedSecretIgnoresAReadFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := sharedStore(t) // migrated, but no secret stored yet

	live := jwtauth.NewSecret([]byte("current-secret-value-32-bytes-ok"))
	tunnels := &stubTunnels{count: 2}

	adoptRotatedSecret(ctx, store, live, tunnels, zap.NewNop())

	if tunnels.stopped {
		t.Fatal("an unreadable backend closed the tunnels")
	}

	if string(live.Get()) != "current-secret-value-32-bytes-ok" {
		t.Fatal("the live secret was replaced from a failed read")
	}
}

// The watcher is only meaningful on a shared backend; a file-backed
// hub is the sole writer of its own secret.
func TestWatchIdentitySkipsFileBackedHubs(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tunnels := &stubTunnels{}

	// Returns immediately without starting a goroutine; the assertion
	// is simply that it does not panic or block on a file store.
	watchIdentity(ctx, hubsecret.NewFile(t.TempDir()), jwtauth.NewSecret([]byte("x")), tunnels, zap.NewNop())

	if tunnels.stopped {
		t.Fatal("a file-backed hub closed tunnels on start")
	}
}
