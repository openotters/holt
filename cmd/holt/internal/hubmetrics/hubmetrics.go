// Package hubmetrics holds the OTel instruments the hub CLI records
// outside the library: the proxy data plane, refused attaches, and
// build info. The holt library owns the tunnel lifecycle counters under
// its own scope.
//
// Instruments are built from the global MeterProvider, so they are
// no-ops until --metrics installs one (see Provider) and live once it
// does. Nothing here needs to know whether metrics are on.
//
// The record methods match the hook signatures the proxy and the attach
// guard take, so wiring is a method value:
//
//	proxy.New(registry, proxy.WithErrorHook(metrics.RecordProxyError))
//	jwtauth.Guard{Secret: secret, OnReject: metrics.RecordReject}
package hubmetrics

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	promexporter "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// scope is the OTel instrumentation scope for the CLI-side hub
// instruments.
const scope = "github.com/openotters/holt/cmd/holt"

// Metrics holds the CLI-side instruments. Build it with New.
type Metrics struct {
	proxyRequests metric.Int64Counter       // by status code
	proxyDuration metric.Float64Histogram   // request duration, seconds
	proxyInflight metric.Int64UpDownCounter // in-flight proxied requests
	proxyErrors   metric.Int64Counter       // by reason
	rejected      metric.Int64Counter       // rejected attaches, by reason
}

// New builds the CLI instruments and registers a build_info gauge that
// always reports 1 with the version/commit labels.
func New(version, commit string) *Metrics {
	meter := otel.GetMeterProvider().Meter(scope)

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

	return &Metrics{
		proxyRequests: reqs,
		proxyDuration: dur,
		proxyInflight: inflight,
		proxyErrors:   errs,
		rejected:      rejected,
	}
}

// RecordReject counts a refused attach with a low-cardinality reason.
// Its signature is jwtauth.RejectHook.
func (m *Metrics) RecordReject(ctx context.Context, reason string) {
	m.rejected.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
}

// RecordProxyError counts a proxy routing failure with its reason. Its
// signature is proxy.ErrorHook.
func (m *Metrics) RecordProxyError(ctx context.Context, reason string) {
	m.proxyErrors.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
}

// Instrument wraps a proxy handler with in-flight, duration, and
// per-status-code request counters.
func (m *Metrics) Instrument(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.proxyInflight.Add(r.Context(), 1)
		defer m.proxyInflight.Add(r.Context(), -1)

		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		attrs := metric.WithAttributes(attribute.Int("code", rec.status))
		m.proxyRequests.Add(r.Context(), 1, attrs)
		m.proxyDuration.Record(r.Context(), time.Since(start).Seconds(), attrs)
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

// Provider builds an OTel SDK meter provider backed by a Prometheus
// exporter (registered with the default Prometheus registry that
// Handler serves). Install it globally with otel.SetMeterProvider
// before building any instrument, so everything binds to it.
func Provider() (*sdkmetric.MeterProvider, error) {
	exporter, err := promexporter.New()
	if err != nil {
		return nil, fmt.Errorf("metrics: %w", err)
	}

	return sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter)), nil
}

// Handler serves the Prometheus exposition of everything the exporter
// from Provider has collected.
func Handler() http.Handler { return promhttp.Handler() }
