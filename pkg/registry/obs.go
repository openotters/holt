package registry

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/openotters/holt/pkg/tunneltype"
)

// instrumentName is the OTel instrumentation scope for this module.
const instrumentName = "github.com/openotters/holt/pkg/registry"

// metrics holds the OTel instruments the Registry records against,
// built from a MeterProvider defaulting to the global (no-op) one.
type metrics struct {
	attaches metric.Int64Counter // total attaches
	detaches metric.Int64Counter // total detaches, by reason
}

// newMetrics builds the instruments. Creation errors collapse into
// no-op instruments: a metrics backend must never break tunnel
// handling.
func newMetrics(mp metric.MeterProvider, activeFn func() map[string]int64) *metrics {
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
			// Every carried type is observed on every scrape, at zero
			// when none is attached. Observing only what is live would
			// make the series disappear on an idle hub, and a graph of
			// "no data" reads as broken rather than as zero.
			live := activeFn()
			for _, kind := range []tunneltype.Type{tunneltype.HTTP, tunneltype.HTTPS} {
				o.Observe(live[kind.String()], metric.WithAttributes(attribute.String("type", kind.String())))
			}

			return nil
		}))

	// Seed the attaches counter so the series shows at 0 before the
	// first attach, instead of being absent on a fresh hub. Seeded per
	// type, or the seed would sit as an unlabelled series beside the
	// labelled ones.
	for _, kind := range []tunneltype.Type{tunneltype.HTTP, tunneltype.HTTPS} {
		attaches.Add(context.Background(), 0, metric.WithAttributes(attribute.String("type", kind.String())))
	}

	return &metrics{attaches: attaches, detaches: detaches}
}

func (m *metrics) recordAttach(ctx context.Context, kind string) {
	if m == nil {
		return
	}

	m.attaches.Add(ctx, 1, metric.WithAttributes(attribute.String("type", kind)))
}

func (m *metrics) recordDetach(ctx context.Context, reason, kind string) {
	if m == nil {
		return
	}

	m.detaches.Add(ctx, 1, metric.WithAttributes(
		attribute.String("reason", reason), attribute.String("type", kind)))
}
