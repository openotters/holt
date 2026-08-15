package peername_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/openotters/holt/cmd/holt/internal/peername"
)

// The shape the CLI documents and the hub has to accept.
var randomShape = regexp.MustCompile(`^[a-z]+-[a-z]+-[0-9a-f]{6}$`)

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

		if !randomShape.MatchString(name) {
			t.Fatalf("Random() = %q, want two words and a six-hex suffix", name)
		}
	}
}

// The suffix is what makes a name safe to use without asking the hub
// whether it is taken, so two draws must not collide in practice.
func TestRandomDoesNotRepeat(t *testing.T) {
	t.Parallel()

	const draws = 5000

	seen := make(map[string]bool, draws)

	for range draws {
		name, err := peername.Random()
		if err != nil {
			t.Fatal(err)
		}

		if seen[name] {
			t.Fatalf("Random() repeated %q within %d draws", name, draws)
		}

		seen[name] = true
	}
}

// The whole point of the suffix: it is what separates two peers that
// happen to draw the same two words, so it has to actually vary.
func TestRandomSuffixVaries(t *testing.T) {
	t.Parallel()

	const draws = 500

	suffixes := make(map[string]bool, draws)

	for range draws {
		name, err := peername.Random()
		if err != nil {
			t.Fatal(err)
		}

		suffixes[name[strings.LastIndex(name, "-")+1:]] = true
	}

	// Over 24 bits, 500 draws should be all but entirely distinct;
	// anything near-constant means the suffix is not carrying its
	// weight.
	if len(suffixes) < draws-5 {
		t.Fatalf("%d draws produced only %d distinct suffixes", draws, len(suffixes))
	}
}

// A name has to stay comfortably inside the DNS label limit.
func TestRandomStaysShort(t *testing.T) {
	t.Parallel()

	for range 200 {
		name, err := peername.Random()
		if err != nil {
			t.Fatal(err)
		}

		if len(name) > 32 {
			t.Fatalf("Random() = %q, %d characters: too long to say out loud", name, len(name))
		}
	}
}
