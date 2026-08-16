package hubsecret_test

import (
	"bytes"
	"context"
	"database/sql"
	"sync"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/openotters/holt/cmd/holt/internal/hubsecret"
)

// openSQLite gives each test its own in-memory database, shared across
// the pool's single connection for the test's lifetime.
func openSQLite(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}

	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	return db
}

func newStore(t *testing.T, db *sql.DB, opts ...hubsecret.SQLOption) *hubsecret.SQLStore {
	t.Helper()

	store := hubsecret.NewSQL(db, hubsecret.SQLite, opts...)
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	return store
}

func TestSQLLoadOrCreateIsStable(t *testing.T) {
	t.Parallel()

	store := newStore(t, openSQLite(t))
	ctx := context.Background()

	first, err := store.LoadOrCreate(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(first) == 0 {
		t.Fatal("secret should not be empty")
	}

	second, err := store.LoadOrCreate(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(first, second) {
		t.Fatal("a second call must reuse the stored secret, or restarts would invalidate tokens")
	}
}

// The reason LoadOrCreate inserts conditionally and reads back: hubs
// booting together must end up with ONE secret. Two would split the
// fleet, each half refusing the other's tokens.
func TestSQLConcurrentCreateConverges(t *testing.T) {
	t.Parallel()

	db := openSQLite(t)
	newStore(t, db) // migrate once

	const hubs = 8

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results [][]byte
	)

	for range hubs {
		wg.Add(1)

		go func() {
			defer wg.Done()

			secret, err := hubsecret.NewSQL(db, hubsecret.SQLite).LoadOrCreate(context.Background())
			if err != nil {
				t.Error(err)

				return
			}

			mu.Lock()
			results = append(results, secret)
			mu.Unlock()
		}()
	}

	wg.Wait()

	if len(results) != hubs {
		t.Fatalf("got %d results, want %d", len(results), hubs)
	}

	for i, got := range results {
		if !bytes.Equal(got, results[0]) {
			t.Fatalf("hub %d ended up with a different secret; the fleet is split", i)
		}
	}
}

// A hub moving off its state volume keeps the identity it had, so
// tokens already in the field survive the move.
func TestSQLAdoptsTheSeededSecret(t *testing.T) {
	t.Parallel()

	existing := bytes.Repeat([]byte{0xA5}, 32)

	store := newStore(t, openSQLite(t), hubsecret.WithSeed(existing))

	got, err := store.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, existing) {
		t.Fatal("the file secret was not adopted; every issued token would be invalid")
	}
}

// ...but a seed never overwrites a secret the fleet already agreed on.
func TestSQLSeedIsIgnoredOnceStored(t *testing.T) {
	t.Parallel()

	db := openSQLite(t)
	ctx := context.Background()

	stored, err := newStore(t, db).LoadOrCreate(ctx)
	if err != nil {
		t.Fatal(err)
	}

	seeded, err := hubsecret.NewSQL(db, hubsecret.SQLite,
		hubsecret.WithSeed(bytes.Repeat([]byte{0x11}, 32))).LoadOrCreate(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(seeded, stored) {
		t.Fatal("a joining hub's own file secret overwrote the shared one")
	}
}

func TestSQLLoadErrorsWithoutSecret(t *testing.T) {
	t.Parallel()

	if _, err := newStore(t, openSQLite(t)).Load(context.Background()); err == nil {
		t.Fatal("Load should error when no secret is stored yet")
	}
}

func TestSQLRotateReplacesForEveryone(t *testing.T) {
	t.Parallel()

	db := openSQLite(t)
	ctx := context.Background()

	hubA := newStore(t, db)
	hubB := hubsecret.NewSQL(db, hubsecret.SQLite)

	before, err := hubA.LoadOrCreate(ctx)
	if err != nil {
		t.Fatal(err)
	}

	after, err := hubA.Rotate(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Equal(before, after) {
		t.Fatal("Rotate must produce a different secret")
	}

	// The other hub reads the rotation, which is what makes rotating
	// fleet-wide rather than per-hub.
	seenByB, err := hubB.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(seenByB, after) {
		t.Fatal("the second hub still sees the old secret; the fleet disagrees on identity")
	}
}

func TestSQLMigrateIsIdempotent(t *testing.T) {
	t.Parallel()

	db := openSQLite(t)
	store := hubsecret.NewSQL(db, hubsecret.SQLite)

	for range 3 {
		if err := store.Migrate(context.Background()); err != nil {
			t.Fatalf("migrate: %v", err)
		}
	}
}
