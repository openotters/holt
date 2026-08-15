package peername_test

import (
	"strings"
	"testing"

	"github.com/openotters/holt/cmd/holt/internal/peername"
)

// Every generated name has to satisfy the rule the hub enforces,
// otherwise expose would mint a token the hub then refuses.
func TestRandomIsAlwaysAValidPeerName(t *testing.T) {
	t.Parallel()

	for range 500 {
		name, err := peername.Random()
		if err != nil {
			t.Fatal(err)
		}

		if validateErr := peername.Validate(name); validateErr != nil {
			t.Fatalf("Random() = %q, which is not a valid peer name: %v", name, validateErr)
		}

		if !strings.Contains(name, "-") {
			t.Fatalf("Random() = %q, want two words joined by a dash", name)
		}
	}
}

// The point of Unique: never hand back a name that is already
// attached, since attaching under it would evict that peer.
func TestUniqueAvoidsTakenNames(t *testing.T) {
	t.Parallel()

	// Seed the taken set with a large sample of real draws, so the
	// generator has to work around them.
	taken := map[string]bool{}

	for range 300 {
		name, err := peername.Random()
		if err != nil {
			t.Fatal(err)
		}

		taken[name] = true
	}

	for range 200 {
		name, err := peername.Unique(taken)
		if err != nil {
			t.Fatal(err)
		}

		if taken[name] {
			t.Fatalf("Unique() returned %q, which is taken", name)
		}

		if validateErr := peername.Validate(name); validateErr != nil {
			t.Fatalf("Unique() = %q, not a valid peer name: %v", name, validateErr)
		}
	}
}

// With everything taken, Unique must still terminate with a valid,
// unused name rather than loop or give up.
func TestUniqueFallsBackWhenEverythingCollides(t *testing.T) {
	t.Parallel()

	// A set that claims every two-word name is taken forces the
	// suffix path.
	everything := takenEverything{}

	name, err := peername.Unique(everything.set(t))
	if err != nil {
		t.Fatal(err)
	}

	if validateErr := peername.Validate(name); validateErr != nil {
		t.Fatalf("fallback name %q is invalid: %v", name, validateErr)
	}

	// brisk-otter-a3f1: the suffix makes it three segments.
	if strings.Count(name, "-") < 2 {
		t.Fatalf("fallback name %q should carry a suffix", name)
	}
}

type takenEverything struct{}

// set builds a map that reports every plain two-word draw as taken,
// by exhausting a big sample of them.
func (takenEverything) set(t *testing.T) map[string]bool {
	t.Helper()

	taken := map[string]bool{}

	// The word lists are unexported; sample heavily enough that the
	// 12 attempts inside Unique all collide. 20k draws over ~4k
	// combinations covers the space many times over.
	for range 20000 {
		name, err := peername.Random()
		if err != nil {
			t.Fatal(err)
		}

		taken[name] = true
	}

	return taken
}

func TestUniqueWithNoKnownTakenNames(t *testing.T) {
	t.Parallel()

	name, err := peername.Unique(nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := peername.Validate(name); err != nil {
		t.Fatalf("Unique(nil) = %q, not valid: %v", name, err)
	}
}
