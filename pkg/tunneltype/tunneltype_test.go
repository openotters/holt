package tunneltype_test

import (
	"testing"

	holtv1 "github.com/openotters/holt/api/v1"
	"github.com/openotters/holt/pkg/tunneltype"
)

// A peer older than the type field sends UNSPECIFIED. Reading that as
// anything but HTTP would refuse tunnels that used to work.
func TestUnspecifiedIsHTTP(t *testing.T) {
	t.Parallel()

	if got := tunneltype.FromProto(holtv1.TunnelType_TUNNEL_TYPE_UNSPECIFIED); got != tunneltype.HTTP {
		t.Fatalf("unspecified read as %q, want http", got)
	}
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()

	for _, kind := range []tunneltype.Type{tunneltype.HTTP, tunneltype.HTTPS, tunneltype.TCP, tunneltype.TLS} {
		if got := tunneltype.FromProto(kind.Proto()); got != kind {
			t.Fatalf("%q round-tripped to %q", kind, got)
		}
	}
}

// The hub carries HTTP and HTTPS today; TCP and TLS are named so they
// can be refused by name rather than failing later.
func TestCarried(t *testing.T) {
	t.Parallel()

	carried := map[tunneltype.Type]bool{
		tunneltype.HTTP:  true,
		tunneltype.HTTPS: true,
		tunneltype.TCP:   false,
		tunneltype.TLS:   false,
	}

	for kind, want := range carried {
		if got := kind.Carried(); got != want {
			t.Fatalf("%q carried = %v, want %v", kind, got, want)
		}
	}
}

// The string reaches a metric label, so it must come from the fixed
// set and never from peer-supplied text.
func TestUnknownProtoFallsBackToHTTP(t *testing.T) {
	t.Parallel()

	if got := tunneltype.FromProto(holtv1.TunnelType(99)); got != tunneltype.HTTP {
		t.Fatalf("unknown enum read as %q, want http", got)
	}
}
