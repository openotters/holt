package peername

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// Random names are two words and a short random suffix, joined by
// dashes, which reads and types better than a UUID and is still a
// valid DNS label:
//
//	cosy-eddy-aec23e, brisk-otter-1f04b9, amber-heron-77c3d1
//
// The words are river-and-woodland themed, a holt being an otter's
// den. The suffix is what carries uniqueness: 24 bits over roughly
// four thousand word pairs, so a name is free to be generated
// without asking the hub whether it is taken. That matters because
// attaching under a name already in use evicts that peer, and a
// client should not have to enumerate other people's tunnels (which
// it may not be allowed to do) just to pick a safe name for its own.
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

// suffixBytes is the random tail, in bytes: 3 gives six hex
// characters, 24 bits. Across the word pairs that is far more room
// than a hub will ever hold peers, and it keeps the whole name short
// enough to say out loud.
const suffixBytes = 3

// Random returns a name like "cosy-eddy-aec23e". It is always a valid
// peer name, and unique in practice without consulting the hub.
func Random() (string, error) {
	adjective, err := pick(adjectives)
	if err != nil {
		return "", err
	}

	noun, err := pick(nouns)
	if err != nil {
		return "", err
	}

	suffix, err := randomHex()
	if err != nil {
		return "", err
	}

	return adjective + "-" + noun + "-" + suffix, nil
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

// randomHex is the name's unique tail.
func randomHex() (string, error) {
	var b [suffixBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generating a peer name: %w", err)
	}

	return fmt.Sprintf("%x", b), nil
}
