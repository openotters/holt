// Package peername validates peer identities. A peer id has to work
// as a DNS label, because the hub's proxy can route to a peer by
// hostname (<peer>.<proxy-domain>): a name that cannot be a label is
// a peer that some deployments simply cannot reach. Enforcing it at
// mint time (and at attach) keeps every name routable by every
// strategy, whatever the hub is configured with today.
package peername

import (
	"fmt"
	"strings"
)

// MaxLen is the DNS label limit (RFC 1123).
const MaxLen = 63

// Rules is the human-readable constraint, reused in errors and help
// text so the CLI, the API, and the console all say the same thing.
const Rules = "lowercase letters, digits and dashes, starting and ending with a letter or digit, at most 63 characters"

// Validate reports whether name is usable as a peer id, which means
// usable as a DNS label: 1..63 characters of [a-z0-9-], starting and
// ending alphanumeric. Uppercase is rejected rather than folded,
// because hostnames are case-insensitive while peer ids are not: a
// peer enrolled as "Alice" could never be reached at alice.<domain>.
func Validate(name string) error {
	if name == "" {
		return fmt.Errorf("peer name is required (%s)", Rules)
	}

	if len(name) > MaxLen {
		return fmt.Errorf("peer name %q is %d characters, the limit is %d (%s)", name, len(name), MaxLen, Rules)
	}

	if err := validateChars(name); err != nil {
		return err
	}

	return validateBoundaries(name)
}

// validateChars rejects anything outside [a-z0-9-], naming the reason
// for the cases people actually hit.
func validateChars(name string) error {
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		case r >= 'A' && r <= 'Z':
			return uppercaseErr(name)
		case r == '.':
			return fmt.Errorf(
				"peer name %q has a dot, which would nest another level under the proxy domain "+
					"(and past a single-level wildcard certificate) (%s)", name, Rules)
		default:
			return fmt.Errorf("peer name %q has an invalid character %q (%s)", name, string(r), Rules)
		}
	}

	return nil
}

// uppercaseErr points at the lowercase form, but only when that form
// is actually valid: suggesting "my_service" for "My_Service" would
// just be the next error.
func uppercaseErr(name string) error {
	const why = "hostnames are case-insensitive so it could never be reached by subdomain"

	if lower := strings.ToLower(name); Validate(lower) == nil {
		return fmt.Errorf("peer name %q has uppercase; %s, use %q instead", name, why, lower)
	}

	return fmt.Errorf("peer name %q has uppercase; %s (%s)", name, why, Rules)
}

// validateBoundaries enforces the alphanumeric first and last
// character (a label may not start or end with a dash).
func validateBoundaries(name string) error {
	if name[0] == '-' {
		return fmt.Errorf("peer name %q starts with a dash (%s)", name, Rules)
	}

	if name[len(name)-1] == '-' {
		return fmt.Errorf("peer name %q ends with a dash (%s)", name, Rules)
	}

	return nil
}
