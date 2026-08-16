package registry

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// instrumentName is the OTel instrumentation scope for this module.
const instrumentName = "github.com/openotters/holt/internal/registry"

// metrics holds the OTel instruments the Registry records against.
// Built from a MeterProvider that defaults to the global one — which
// is a no-op until the application installs an SDK, so instrumentation
// is always present but never mandatory and never a runtime cost when
// unconfigured.
type metrics struct {
	attaches metric.Int64Counter // total attaches
	detaches metric.Int64Counter // total detaches, by reason
}

// newMetrics builds the instruments. active is an observable gauge
// backed by activeFn (the live tunnel count), so it is always present
// and correct on scrape rather than only after the first attach.
// Instrument-creation errors are swallowed into no-op instruments — a
// metrics backend must never break tunnel handling.
func newMetrics(mp metric.MeterProvider, activeFn func() int64) *metrics {
	if mp == nil {
		mp = otel.GetMeterProvider()
	}

	meter := mp.Meter(instrumentName)

	attaches, _ := meter.Int64Counter("holt.tunnels.attaches",
		metric.WithDescription("Total tunnel attaches"))
	detaches, _ := meter.Int64Counter("holt.tunnels.detaches",
		metric.WithDescription("Total tunnel detaches"))

	_, _ = meter.Int64ObservableGauge("holt.tunnels.active",
		metric.WithDescription("Currently attached reverse tunnels"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(activeFn())

			return nil
		}))

	// Seed the attaches counter so the series shows at 0 before the
	// first attach, instead of being absent on a fresh hub.
	attaches.Add(context.Background(), 0)

	return &metrics{attaches: attaches, detaches: detaches}
}

func (m *metrics) recordAttach(ctx context.Context) {
	if m == nil {
		return
	}

	m.attaches.Add(ctx, 1)
}

func (m *metrics) recordDetach(ctx context.Context, reason string) {
	if m == nil {
		return
	}

	m.detaches.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
}
