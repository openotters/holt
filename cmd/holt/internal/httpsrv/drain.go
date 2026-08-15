package httpsrv

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Drain shuts every started listener down gracefully, giving in-flight
// requests up to grace to finish. It reports whether a second signal
// (a second Ctrl-C during the grace period) forced the close instead:
// the caller decides what to say about that, Drain only decides when to
// stop waiting.
//
// The parent context already consumed the first signal, so Drain
// re-arms its own handler for the duration of the drain.
func (g *Group) Drain(grace time.Duration) bool {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()

	hardStop := make(chan os.Signal, 1)
	signal.Notify(hardStop, os.Interrupt, syscall.SIGTERM)

	defer signal.Stop(hardStop)

	drained := make(chan struct{})

	go func() {
		for _, srv := range g.servers {
			_ = srv.Shutdown(shutdownCtx)
		}

		close(drained)
	}()

	return awaitShutdown(drained, hardStop, func() {
		cancel()

		for _, srv := range g.servers {
			_ = srv.Close()
		}
	})
}

// awaitShutdown blocks until the graceful drain finishes or a second
// signal arrives. On the signal it force-closes and reports true, so a
// second Ctrl-C ends the process now instead of waiting out the grace
// period.
func awaitShutdown(drained <-chan struct{}, hardStop <-chan os.Signal, forceClose func()) bool {
	select {
	case <-drained:
		return false
	case <-hardStop:
		forceClose()

		return true
	}
}
