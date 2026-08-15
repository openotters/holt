package peername

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// Random names are two words joined by a dash, which reads and types
// better than a UUID and is still a valid DNS label:
//
//	brisk-otter, amber-heron, quiet-willow
//
// The words are river-and-woodland themed, a holt being an otter's
// den. Roughly 4,000 combinations, so a name is picked against the
// hub's live tunnels rather than trusted to be free (see Unique):
// attaching under a name already in use would evict that peer.
var (
	adjectives = []string{
		"agile", "amber", "ancient", "autumn", "bold", "brave", "brisk", "calm",
		"chilly", "clever", "cosy", "crimson", "curious", "dapper", "deft", "dusky",
		"eager", "early", "easy", "fleet", "floral", "fluent", "fond", "frosty",
		"gentle", "gilded", "glad", "golden", "hardy", "hazel", "hidden", "humble",
		"jolly", "keen", "kindly", "lively", "lucky", "lunar", "mellow", "merry",
		"misty", "mossy", "nimble", "noble", "patient", "placid", "plucky", "polar",
		"proud", "quiet", "rapid", "rosy", "rustic", "sable", "serene", "silent",
		"silver", "sleek", "snowy", "solar", "spry", "steady", "sunny", "swift",
		"tidy", "tranquil", "velvet", "vivid", "wild", "winter", "wise", "witty",
	}

	nouns = []string{
		"alder", "badger", "beaver", "birch", "brook", "cedar", "cove", "creek",
		"crest", "current", "delta", "drift", "eddy", "estuary", "falls", "fern",
		"ford", "glade", "grove", "harbor", "heron", "holt", "inlet", "island",
		"kelp", "lagoon", "lake", "marsh", "meadow", "mink", "moss", "otter",
		"pebble", "pike", "pine", "pond", "pool", "rapids", "reed", "ridge",
		"river", "rush", "sedge", "shoal", "shore", "spring", "stone", "stream",
		"thicket", "tide", "trout", "wharf", "willow",
	}
)

// uniqueAttempts bounds the retries before falling back to a suffix.
// With a few thousand combinations and a handful of live peers, one
// draw almost always lands free; the fallback is for a busy hub (or
// one that could not be asked).
const uniqueAttempts = 12

// Random returns a two-word name like "brisk-otter". It is always a
// valid peer name.
func Random() (string, error) {
	adjective, err := pick(adjectives)
	if err != nil {
		return "", err
	}

	noun, err := pick(nouns)
	if err != nil {
		return "", err
	}

	return adjective + "-" + noun, nil
}

// Unique returns a name that is not in taken. A nil or empty map
// means nothing is known to be taken, in which case the first draw is
// returned. After uniqueAttempts collisions it appends a short random
// suffix ("brisk-otter-a3f1"), which always terminates and stays a
// valid label.
func Unique(taken map[string]bool) (string, error) {
	for range uniqueAttempts {
		name, err := Random()
		if err != nil {
			return "", err
		}

		if !taken[name] {
			return name, nil
		}
	}

	name, err := Random()
	if err != nil {
		return "", err
	}

	suffix, err := randomHex()
	if err != nil {
		return "", err
	}

	return name + "-" + suffix, nil
}

// pick chooses one element with crypto/rand, so names are not
// predictable from a seed.
func pick(list []string) (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(list))))
	if err != nil {
		return "", fmt.Errorf("generating a peer name: %w", err)
	}

	return list[n.Int64()], nil
}

// randomHex is four hex characters, the collision fallback's suffix.
func randomHex() (string, error) {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generating a peer name: %w", err)
	}

	return fmt.Sprintf("%x", b), nil
}
