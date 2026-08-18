package revproxy

import (
	"context"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/openotters/holt/pkg/reqlog"
)

// instrumentName is the OTel instrumentation scope for the proxy.
const instrumentName = "github.com/openotters/holt/pkg/revproxy"

// metrics are the data-plane instruments, in the library rather than
// the CLI so anything embedding the proxy gets the same numbers.
// Instrument-creation errors collapse into no-ops: a metrics backend
// must never break the data plane.
type metrics struct {
	requests metric.Int64Counter       // by status code
	duration metric.Float64Histogram   // request duration, seconds
	inflight metric.Int64UpDownCounter // requests in flight
	errors   metric.Int64Counter       // by reason
}

func newMetrics(mp metric.MeterProvider) *metrics {
	if mp == nil {
		mp = otel.GetMeterProvider()
	}

	meter := mp.Meter(instrumentName)

	requests, _ := meter.Int64Counter("holt.proxy.requests",
		metric.WithDescription("Requests proxied to peers, by status code"))
	duration, _ := meter.Float64Histogram("holt.proxy.request.duration",
		metric.WithUnit("s"), metric.WithDescription("Proxied request duration"))
	inflight, _ := meter.Int64UpDownCounter("holt.proxy.inflight",
		metric.WithDescription("In-flight proxied requests"))
	errors, _ := meter.Int64Counter("holt.proxy.errors",
		metric.WithDescription("Proxy routing errors, by reason"))

	return &metrics{requests: requests, duration: duration, inflight: inflight, errors: errors}
}

// recordError counts a request that could not be routed.
func (m *metrics) recordError(ctx context.Context, reason string) {
	m.errors.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
}

// observe wraps one request with the instruments and returns what it
// became, for the caller to report. The status code is the only metric
// attribute: a peer label would multiply every series by the fleet
// size, which is why the peer travels through the hook instead.
func (m *metrics) observe(
	w http.ResponseWriter, r *http.Request, serve func(http.ResponseWriter, *http.Request) string,
	capture ...reqlog.Option,
) (string, *reqlog.Recorder, time.Duration) {
	ctx := r.Context()

	m.inflight.Add(ctx, 1)
	defer m.inflight.Add(ctx, -1)

	start := time.Now()
	rec := reqlog.NewRecorder(w, capture...)

	peer := serve(rec, r)
	took := time.Since(start)

	attrs := metric.WithAttributes(attribute.Int("code", rec.Status()))
	m.requests.Add(ctx, 1, attrs)
	m.duration.Record(ctx, took.Seconds(), attrs)

	return peer, rec, took
}
