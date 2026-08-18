package blocklist_test

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/openotters/holt/pkg/blocklist"
	"github.com/openotters/holt/pkg/blocklist/sqlite"
)

func openSQLite(t *testing.T) *sql.DB {
	t.Helper()

	// A shared-cache in-memory DB keyed by the test name keeps the
	// single connection's schema visible for the test's lifetime.
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}

	db.SetMaxOpenConns(1) // in-memory DB lives in one connection
	t.Cleanup(func() { _ = db.Close() })

	return db
}

func TestList_BlockUnblockOverSQL(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st := sqlite.New(openSQLite(t))

	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	list, err := blocklist.New(ctx, st)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	if list.IsBlocked("mallory") {
		t.Fatal("fresh list blocks mallory")
	}

	list.Block("mallory")

	if !list.IsBlocked("mallory") {
		t.Fatal("blocked peer not refused")
	}

	got := list.Blocked()
	if len(got) != 1 || got[0].Peer != "mallory" || got[0].BlockedAtUnix == 0 {
		t.Fatalf("blocked = %+v", got)
	}

	list.Unblock("mallory")

	if list.IsBlocked("mallory") {
		t.Fatal("unblocked peer still refused")
	}
}

// TestList_SeesAnotherHubsBlock is the fleet scenario: two Lists over
// the same store (two hubs sharing PostgreSQL in production); a block
// issued through one must take effect on the other without a restart.
func TestList_SeesAnotherHubsBlock(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openSQLite(t)
	st := sqlite.New(db)

	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	hubA, err := blocklist.New(ctx, st)
	if err != nil {
		t.Fatal(err)
	}

	hubB, err := blocklist.New(ctx, sqlite.New(db))
	if err != nil {
		t.Fatal(err)
	}

	hubA.Block("mallory")

	if !hubB.IsBlocked("mallory") {
		t.Fatal("hub B does not see hub A's block through the shared store")
	}

	hubA.Unblock("mallory")

	if hubB.IsBlocked("mallory") {
		t.Fatal("hub B still refuses after hub A unblocked")
	}
}

func TestList_KeepsOriginalBlockTime(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st := sqlite.New(openSQLite(t))

	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	if err := st.Block(ctx, "eve", 1_700_000_000); err != nil {
		t.Fatal(err)
	}

	if err := st.Block(ctx, "eve", 1_800_000_000); err != nil {
		t.Fatal(err)
	}

	blocked, err := st.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if blocked["eve"] != 1_700_000_000 {
		t.Fatalf("blocked_at = %d, want the original 1700000000", blocked["eve"])
	}
}
