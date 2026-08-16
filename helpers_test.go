package holt_test

import (
	"testing"
	"time"

	"github.com/openotters/holt/internal/registry"
)

// waitAttached blocks until peer shows up in the registry, failing
// the test after a bounded wait.
func waitAttached(t *testing.T, r *registry.Registry, peer string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for !r.Attached(peer) {
		if time.Now().After(deadline) {
			t.Fatalf("peer %q never attached", peer)
		}

		time.Sleep(10 * time.Millisecond)
	}
}
