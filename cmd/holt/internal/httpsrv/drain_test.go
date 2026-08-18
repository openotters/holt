//nolint:testpackage // awaitShutdown is unexported; a white-box test is the point.
package httpsrv

import (
	"net"
	"net/http"
	"os"
	"syscall"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestAwaitShutdownGracefulDrain(t *testing.T) {
	t.Parallel()

	drained := make(chan struct{})
	close(drained) // drain already finished

	forced := false
	if awaitShutdown(drained, make(chan os.Signal), func() { forced = true }) {
		t.Fatal("graceful drain should report forced=false")
	}

	if forced {
		t.Fatal("forceClose must not run on a graceful drain")
	}
}

func TestAwaitShutdownSecondSignalForces(t *testing.T) {
	t.Parallel()

	hardStop := make(chan os.Signal, 1)
	hardStop <- syscall.SIGINT // second Ctrl-C during the grace period

	closed := false
	if !awaitShutdown(make(chan struct{}), hardStop, func() { closed = true }) {
		t.Fatal("a second signal should report forced=true")
	}

	if !closed {
		t.Fatal("forceClose must run on the second signal")
	}
}

// A started listener answers on its address, and Drain closes it: after
// the group drains, the port stops serving.
func TestGroupStartAndDrain(t *testing.T) {
	t.Parallel()

	g := NewGroup(zap.NewNop())

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	if err := g.Start(t.Context(), "test", "127.0.0.1:0", handler); err != nil {
		t.Fatalf("start: %v", err)
	}

	if forced := g.Drain(2 * time.Second); forced {
		t.Fatal("an idle group should drain gracefully, not report a forced close")
	}
}

// A port already in use fails at Start, not asynchronously later, so
// the hub reports it at boot instead of coming up half-listening.
func TestGroupStartReportsBindFailure(t *testing.T) {
	t.Parallel()

	var lc net.ListenConfig

	taken, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = taken.Close() })

	g := NewGroup(zap.NewNop())
	if startErr := g.Start(t.Context(), "test", taken.Addr().String(), http.NotFoundHandler()); startErr == nil {
		t.Fatal("Start on a taken port should fail")
	}
}
