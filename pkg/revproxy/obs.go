package revproxy

import (
	"context"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// instrumentName is the OTel instrumentation scope for the proxy.
const instrumentName = "github.com/openotters/holt/pkg/revproxy"

// metrics are the data-plane instruments: what the proxy carried, how
// long it took, and what it could not route. They live here rather
// than in the CLI so anything embedding the proxy gets the same
// numbers (and the same dashboard) without rebuilding them.
//
// Built from a MeterProvider that defaults to the global one, which is
// a no-op until the application installs an SDK. Instrument-creation
// errors collapse into no-op instruments: a metrics backend must never
// break the data plane.
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

// observe wraps one request with the in-flight, duration and
// per-status-code counters. The status code is the only attribute:
// a peer label would multiply every series by the fleet size.
func (m *metrics) observe(w http.ResponseWriter, r *http.Request, serve func(http.ResponseWriter, *http.Request)) {
	ctx := r.Context()

	m.inflight.Add(ctx, 1)
	defer m.inflight.Add(ctx, -1)

	start := time.Now()
	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

	serve(rec, r)

	attrs := metric.WithAttributes(attribute.Int("code", rec.status))
	m.requests.Add(ctx, 1, attrs)
	m.duration.Record(ctx, time.Since(start).Seconds(), attrs)
}

// statusRecorder captures the response status code for the metrics.
// It forwards Flush and Unwrap, so streaming responses (the proxy
// flushes immediately) and any handler that unwraps the writer keep
// working through it.
type statusRecorder struct {
	http.ResponseWriter
	status  int
	written bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.written {
		s.status = code
		s.written = true
	}

	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	s.written = true

	return s.ResponseWriter.Write(b)
}

// Flush keeps the proxy's immediate flushing working: without it the
// wrapper would hide the underlying Flusher and responses would buffer.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the real writer (hijack,
// deadlines), which the standard library looks for.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }
