package commands

import (
	"context"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// metricsScope is the OTel instrumentation scope for the CLI-side hub
// instruments (proxy path, auth rejects, build info). The library owns
// the tunnel lifecycle counters under its own scope.
const metricsScope = "github.com/openotters/holt/cmd/holt"

// hubMetrics holds the instruments the hub records outside the
// registry: the proxy data plane, rejected attaches, and build info.
// Built from the global MeterProvider, so it is a no-op when --metrics
// is off and live when it is on.
type hubMetrics struct {
	proxyRequests metric.Int64Counter       // by status code
	proxyDuration metric.Float64Histogram   // request duration, seconds
	proxyInflight metric.Int64UpDownCounter // in-flight proxied requests
	proxyErrors   metric.Int64Counter       // by reason
	rejected      metric.Int64Counter       // rejected attaches, by reason
}

// newHubMetrics builds the CLI instruments and registers a build_info
// gauge that always reports 1 with the version/commit labels.
func newHubMetrics(version, commit string) *hubMetrics {
	meter := otel.GetMeterProvider().Meter(metricsScope)

	reqs, _ := meter.Int64Counter("holt.proxy.requests",
		metric.WithDescription("Requests proxied to peers, by status code"))
	dur, _ := meter.Float64Histogram("holt.proxy.request.duration",
		metric.WithUnit("s"), metric.WithDescription("Proxied request duration"))
	inflight, _ := meter.Int64UpDownCounter("holt.proxy.inflight",
		metric.WithDescription("In-flight proxied requests"))
	errs, _ := meter.Int64Counter("holt.proxy.errors",
		metric.WithDescription("Proxy routing errors, by reason"))
	rejected, _ := meter.Int64Counter("holt.tunnels.rejected",
		metric.WithDescription("Rejected tunnel attaches, by reason"))

	_, _ = meter.Int64ObservableGauge("holt.build.info",
		metric.WithDescription("Build info; value is always 1"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(1, metric.WithAttributes(
				attribute.String("version", version),
				attribute.String("commit", commit)))

			return nil
		}))

	return &hubMetrics{
		proxyRequests: reqs,
		proxyDuration: dur,
		proxyInflight: inflight,
		proxyErrors:   errs,
		rejected:      rejected,
	}
}

// recordReject counts a rejected attach with a low-cardinality reason.
func (m *hubMetrics) recordReject(ctx context.Context, reason string) {
	m.rejected.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
}

// recordProxyError counts a proxy routing failure with its reason.
func (m *hubMetrics) recordProxyError(ctx context.Context, reason string) {
	m.proxyErrors.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
}

// instrument wraps a proxy handler with in-flight, duration, and
// per-status-code request counters.
func (m *hubMetrics) instrument(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.proxyInflight.Add(r.Context(), 1)
		defer m.proxyInflight.Add(r.Context(), -1)

		start := timeNow()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		attrs := metric.WithAttributes(attribute.Int("code", rec.status))
		m.proxyRequests.Add(r.Context(), 1, attrs)
		m.proxyDuration.Record(r.Context(), timeSince(start), attrs)
	})
}

// statusRecorder captures the response status code for metrics.
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

func timeNow() time.Time            { return time.Now() }
func timeSince(t time.Time) float64 { return time.Since(t).Seconds() }
