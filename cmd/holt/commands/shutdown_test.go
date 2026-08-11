//nolint:testpackage // awaitShutdown is unexported; a white-box test is the point.
package commands

import (
	"os"
	"syscall"
	"testing"
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
