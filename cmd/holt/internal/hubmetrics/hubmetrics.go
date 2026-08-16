// Package hubmetrics holds the OTel instruments the hub CLI records
// outside the library: refused attaches, and build info. The library
// owns the rest under its own scopes, the tunnel lifecycle in
// registry and the data plane in revproxy, so a program embedding
// them gets the same numbers without rebuilding any of this.
//
// Instruments are built from the global MeterProvider, so they are
// no-ops until --metrics installs one (see Provider) and live once it
// does. Nothing here needs to know whether metrics are on.
//
// The record method matches the hook signature the attach guard takes,
// so wiring is a method value:
//
//	jwtauth.Guard{Secret: secret, OnReject: metrics.RecordReject}
package hubmetrics

import (
	"context"
	"fmt"
	"net/http"

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
	rejected metric.Int64Counter // rejected attaches, by reason
}

// New builds the CLI instruments and registers a build_info gauge that
// always reports 1 with the version/commit labels.
func New(version, commit string) *Metrics {
	meter := otel.GetMeterProvider().Meter(scope)

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
		rejected: rejected,
	}
}

// RecordReject counts a refused attach with a low-cardinality reason.
// Its signature is jwtauth.RejectHook.
func (m *Metrics) RecordReject(ctx context.Context, reason string) {
	m.rejected.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
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
