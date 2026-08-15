package peername_test

import (
	"strings"
	"testing"

	"github.com/openotters/holt/cmd/holt/internal/peername"
)

func TestValidateAccepts(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"a",
		"web",
		"web-1",
		"api-gateway-eu-west-1",
		"0",
		"9lives",
		strings.Repeat("a", peername.MaxLen),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := peername.Validate(name); err != nil {
				t.Fatalf("Validate(%q) = %v, want nil", name, err)
			}
		})
	}
}

func TestValidateRejects(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"empty":         "",
		"uppercase":     "Alice",
		"underscore":    "my_service",
		"dot":           "a.b",
		"space":         "my service",
		"leading dash":  "-web",
		"trailing dash": "web-",
		"slash":         "a/b",
		"at sign":       "svc@host",
		"unicode":       "wéb",
		"too long":      strings.Repeat("a", peername.MaxLen+1),
	}

	for label, name := range cases {
		t.Run(label, func(t *testing.T) {
			t.Parallel()

			if err := peername.Validate(name); err == nil {
				t.Fatalf("Validate(%q) = nil, want an error", name)
			}
		})
	}
}

// The uppercase message points at the fix, since it is the most
// likely honest mistake.
func TestValidateUppercaseSuggestsLowercase(t *testing.T) {
	t.Parallel()

	err := peername.Validate("MyService")
	if err == nil {
		t.Fatal("expected an error")
	}

	if !strings.Contains(err.Error(), `"myservice"`) {
		t.Fatalf("error %q does not suggest the lowercase form", err)
	}
}

// ...but only when lowercasing would actually fix it: "My_Service"
// lowercases to "my_service", which is still invalid, so suggesting
// it would just be the next error.
func TestValidateUppercaseSuggestsNothingUnfixable(t *testing.T) {
	t.Parallel()

	err := peername.Validate("My_Service")
	if err == nil {
		t.Fatal("expected an error")
	}

	if strings.Contains(err.Error(), `"my_service"`) {
		t.Fatalf("error %q suggests a name that is still invalid", err)
	}
}
