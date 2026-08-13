//nolint:testpackage // openDirectory and redactDSN are unexported; white-box unit test on purpose.
package commands

import (
	"context"
	"testing"
	"time"

	"github.com/openotters/holt/cmd/holt/internal/store"
)

func TestOpenDirectory_DefaultSQLite(t *testing.T) {
	t.Parallel()

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	h := &Hub{}

	dir, closeDir, err := h.openDirectory(context.Background(), st)
	if err != nil {
		t.Fatalf("openDirectory: %v", err)
	}
	defer closeDir()

	// The SQLite directory shares the store's DB: migrating and listing
	// must work without any external database.
	if migErr := dir.Migrate(context.Background()); migErr != nil {
		t.Fatalf("migrate: %v", migErr)
	}

	if _, listErr := dir.List(context.Background()); listErr != nil {
		t.Fatalf("list: %v", listErr)
	}
}

func TestOpenDirectory_UnreachablePostgres(t *testing.T) {
	t.Parallel()

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	// Reserved TEST-NET address: the ping must fail fast at boot, not
	// on the first attach.
	h := &Hub{DirectoryDSN: "postgres://u:p@192.0.2.1:1/holt?connect_timeout=1"}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, _, dirErr := h.openDirectory(ctx, st); dirErr == nil {
		t.Fatal("expected an error for an unreachable postgres")
	}
}

func TestRedactDSN(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"postgres://user:secret@db.example.com:5432/holt": "postgres://user:xxxxx@db.example.com:5432/holt",
		"postgres://db.example.com/holt":                  "postgres://db.example.com/holt",
		"host=db user=u password=secret dbname=holt":      "postgres (DSN hidden)",
	}

	for dsn, want := range cases {
		if got := redactDSN(dsn); got != want {
			t.Errorf("redactDSN(%q) = %q, want %q", dsn, got, want)
		}
	}
}
