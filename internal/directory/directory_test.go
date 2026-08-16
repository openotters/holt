package directory_test

import (
	"context"
	"testing"
	"time"

	"github.com/openotters/holt/internal/directory"
)

func TestMemoryDirectory(t *testing.T) {
	t.Parallel()

	testDirectory(t, directory.NewMemoryDirectory())
}

// testDirectory is the shared contract every Directory implementation
// must satisfy — reused by the SQL directory's tests.
func testDirectory(t *testing.T, dir directory.Directory) {
	t.Helper()

	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0)

	testAttachAndUpsert(ctx, t, dir, now)
	testDetachOwnership(ctx, t, dir)
	testListAndClearHub(ctx, t, dir, now)
}

// testAttachAndUpsert covers attach, lookup, and re-attach replacing
// ownership.
func testAttachAndUpsert(ctx context.Context, t *testing.T, dir directory.Directory, now time.Time) {
	t.Helper()

	rec := directory.PeerRecord{Peer: "alice", Hub: "hub-a", PeerVersion: "v1", AttachedAt: now}
	if err := dir.Attach(ctx, rec); err != nil {
		t.Fatalf("attach: %v", err)
	}

	got, ok, err := dir.Lookup(ctx, "alice")
	if err != nil || !ok {
		t.Fatalf("lookup: %v, ok=%v", err, ok)
	}
	if got.Hub != "hub-a" || got.PeerVersion != "v1" || !got.AttachedAt.Equal(now) {
		t.Fatalf("record = %+v", got)
	}

	// Upsert: a re-attach to a different hub replaces ownership.
	reattach := directory.PeerRecord{Peer: "alice", Hub: "hub-b", PeerVersion: "v2", AttachedAt: now}
	if err := dir.Attach(ctx, reattach); err != nil {
		t.Fatalf("re-attach: %v", err)
	}

	got, _, _ = dir.Lookup(ctx, "alice")
	if got.Hub != "hub-b" {
		t.Fatalf("owner after re-attach = %q, want hub-b", got.Hub)
	}
}

// testDetachOwnership covers stale and owning detaches; it expects
// "alice" to be owned by hub-b (testAttachAndUpsert leaves it there).
func testDetachOwnership(ctx context.Context, t *testing.T, dir directory.Directory) {
	t.Helper()

	// A stale detach from the OLD owner must not evict the new one.
	if err := dir.Detach(ctx, "alice", "hub-a"); err != nil {
		t.Fatalf("stale detach: %v", err)
	}
	if _, ok, _ := dir.Lookup(ctx, "alice"); !ok {
		t.Fatal("stale detach evicted the current owner")
	}

	// The real owner detaches.
	if err := dir.Detach(ctx, "alice", "hub-b"); err != nil {
		t.Fatalf("detach: %v", err)
	}
	if _, ok, _ := dir.Lookup(ctx, "alice"); ok {
		t.Fatal("record survived its owner's detach")
	}
}

// testListAndClearHub covers ordered listing and per-hub cleanup.
func testListAndClearHub(ctx context.Context, t *testing.T, dir directory.Directory, now time.Time) {
	t.Helper()

	_ = dir.Attach(ctx, directory.PeerRecord{Peer: "b", Hub: "hub-a", AttachedAt: now})
	_ = dir.Attach(ctx, directory.PeerRecord{Peer: "a", Hub: "hub-a", AttachedAt: now})
	_ = dir.Attach(ctx, directory.PeerRecord{Peer: "c", Hub: "hub-b", AttachedAt: now})

	list, err := dir.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 || list[0].Peer != "a" || list[1].Peer != "b" || list[2].Peer != "c" {
		t.Fatalf("list not ordered: %+v", list)
	}

	if err := dir.ClearHub(ctx, "hub-a"); err != nil {
		t.Fatalf("clearhub: %v", err)
	}

	list, _ = dir.List(ctx)
	if len(list) != 1 || list[0].Peer != "c" {
		t.Fatalf("after ClearHub(hub-a): %+v", list)
	}
}
