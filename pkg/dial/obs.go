package dial

import (
	"context"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// instrumentName is the OTel instrumentation scope for the peer side.
const instrumentName = "github.com/openotters/holt/pkg/dial"

// Attach failure reasons, the only attribute on the failure counter.
// They cover attempts that never became a tunnel: a session that ends
// after attaching is not a failure (a clean shutdown ends one too), so
// it is the session-duration histogram that records it.
const (
	reasonDial      = "dial"      // the WebSocket never opened
	reasonHandshake = "handshake" // opened, but Hello/Welcome failed
)

// metrics are the peer's own view of its tunnel, which is the view
// that answers "is this peer flapping?". A hub only sees the attaches
// that reached it; the peer sees the failures too, and how long each
// session lasted before it had to redial.
//
// Built from a MeterProvider that defaults to the global one, so the
// instruments are always present and cost nothing until an SDK is
// installed.
type metrics struct {
	attaches metric.Int64Counter     // successful attaches
	failures metric.Int64Counter     // failed attempts, by reason
	sessions metric.Float64Histogram // how long a tunnel lasted, seconds

	// attached is 1 while a tunnel is up, read by an observable gauge
	// on every scrape rather than written on a schedule.
	attached atomic.Int64
}

func newMetrics(mp metric.MeterProvider) *metrics {
	if mp == nil {
		mp = otel.GetMeterProvider()
	}

	meter := mp.Meter(instrumentName)
	m := &metrics{}

	m.attaches, _ = meter.Int64Counter("holt.peer.attaches",
		metric.WithDescription("Tunnels this peer attached"))
	m.failures, _ = meter.Int64Counter("holt.peer.attach.failures",
		metric.WithDescription("Attach attempts that failed, by reason"))
	m.sessions, _ = meter.Float64Histogram("holt.peer.session.duration",
		metric.WithUnit("s"), metric.WithDescription("How long an attached tunnel lasted"))

	_, _ = meter.Int64ObservableGauge("holt.peer.attached",
		metric.WithDescription("1 while this peer has a live tunnel, 0 otherwise"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(m.attached.Load())

			return nil
		}))

	// Seed the counter so the series exists at 0 before the first
	// attach, instead of being absent on a peer that cannot connect.
	m.attaches.Add(context.Background(), 0)

	return m
}

// recordAttach marks the tunnel up.
func (m *metrics) recordAttach(ctx context.Context) {
	m.attaches.Add(ctx, 1)
	m.attached.Store(1)
}

// recordDetach marks the tunnel down and records how long it lasted,
// which is what separates a stable peer from one redialing in a loop.
func (m *metrics) recordDetach(ctx context.Context, since time.Time) {
	m.attached.Store(0)
	m.sessions.Record(ctx, time.Since(since).Seconds())
}

// recordFailure counts an attempt that never became a tunnel.
func (m *metrics) recordFailure(ctx context.Context, reason string) {
	m.failures.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
}
