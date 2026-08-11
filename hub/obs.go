package hub

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// instrumentName is the OTel instrumentation scope for this module.
const instrumentName = "github.com/openotters/holt/hub"

// metrics holds the OTel instruments the Registry records against.
// Built from a MeterProvider that defaults to the global one — which
// is a no-op until the application installs an SDK, so instrumentation
// is always present but never mandatory and never a runtime cost when
// unconfigured.
type metrics struct {
	active   metric.Int64UpDownCounter // currently-attached tunnels
	attaches metric.Int64Counter       // total attaches
	detaches metric.Int64Counter       // total detaches, by reason
}

// newMetrics builds the instruments. Instrument-creation errors are
// swallowed into no-op instruments — a metrics backend must never
// break tunnel handling.
func newMetrics(mp metric.MeterProvider) *metrics {
	if mp == nil {
		mp = otel.GetMeterProvider()
	}

	meter := mp.Meter(instrumentName)

	active, _ := meter.Int64UpDownCounter("holt.tunnels.active",
		metric.WithDescription("Currently attached reverse tunnels"))
	attaches, _ := meter.Int64Counter("holt.tunnels.attaches",
		metric.WithDescription("Total tunnel attaches"))
	detaches, _ := meter.Int64Counter("holt.tunnels.detaches",
		metric.WithDescription("Total tunnel detaches"))

	return &metrics{active: active, attaches: attaches, detaches: detaches}
}

func (m *metrics) recordAttach(ctx context.Context) {
	if m == nil {
		return
	}

	m.active.Add(ctx, 1)
	m.attaches.Add(ctx, 1)
}

func (m *metrics) recordDetach(ctx context.Context, reason string) {
	if m == nil {
		return
	}

	m.active.Add(ctx, -1)
	m.detaches.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
}

// tracer returns the tracer for handler spans, defaulting to the
// global TracerProvider (no-op until an SDK is installed).
func tracer(tp trace.TracerProvider) trace.Tracer {
	if tp == nil {
		tp = otel.GetTracerProvider()
	}

	return tp.Tracer(instrumentName)
}
