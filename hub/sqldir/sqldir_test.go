package sqldir_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/openotters/holt/hub"
	"github.com/openotters/holt/hub/sqldir"
)

// openSQLite opens a fresh in-memory SQLite DB for one test.
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

func TestSQLDirectory_Contract(t *testing.T) {
	t.Parallel()

	db := openSQLite(t)
	dir := sqldir.New(db, sqldir.SQLite)

	if err := dir.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	testDirectoryContract(t, dir)
}

func TestSQLDirectory_MigrateIdempotent(t *testing.T) {
	t.Parallel()

	db := openSQLite(t)
	dir := sqldir.New(db, sqldir.SQLite)

	for range 3 {
		if err := dir.Migrate(context.Background()); err != nil {
			t.Fatalf("migrate: %v", err)
		}
	}
}

// testDirectoryContract mirrors the hub package's directory contract
// (kept local so this package needn't export it).
func testDirectoryContract(t *testing.T, dir hub.Directory) {
	t.Helper()

	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0)

	if err := dir.Attach(ctx, hub.PeerRecord{Peer: "alice", Hub: "a", PeerVersion: "v1", AttachedAt: now}); err != nil {
		t.Fatalf("attach: %v", err)
	}

	got, ok, err := dir.Lookup(ctx, "alice")
	if err != nil || !ok {
		t.Fatalf("lookup: %v ok=%v", err, ok)
	}
	if got.Hub != "a" || got.PeerVersion != "v1" || !got.AttachedAt.Equal(now) {
		t.Fatalf("record = %+v", got)
	}

	// Re-attach to a new hub, stale detach must not evict.
	_ = dir.Attach(ctx, hub.PeerRecord{Peer: "alice", Hub: "b", AttachedAt: now})
	_ = dir.Detach(ctx, "alice", "a")
	if _, ok, _ := dir.Lookup(ctx, "alice"); !ok {
		t.Fatal("stale detach evicted current owner")
	}

	_ = dir.Detach(ctx, "alice", "b")
	if _, ok, _ := dir.Lookup(ctx, "alice"); ok {
		t.Fatal("record survived owner detach")
	}

	_ = dir.Attach(ctx, hub.PeerRecord{Peer: "b", Hub: "a", AttachedAt: now})
	_ = dir.Attach(ctx, hub.PeerRecord{Peer: "a", Hub: "a", AttachedAt: now})

	list, err := dir.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 || list[0].Peer != "a" || list[1].Peer != "b" {
		t.Fatalf("list = %+v", list)
	}

	_ = dir.ClearHub(ctx, "a")
	if list, _ := dir.List(ctx); len(list) != 0 {
		t.Fatalf("after ClearHub: %+v", list)
	}
}
