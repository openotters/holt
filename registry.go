package holt

import (
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"github.com/openotters/holt/internal/directory"
	"github.com/openotters/holt/internal/registry"
)

// Registry is the operational surface over a hub's live tunnels:
// roster, stop, watch, and the per-peer RoundTripper that dials
// through a tunnel. A Server exposes its own via Server.Registry;
// NewRegistry builds one to share or configure up front (see
// WithRegistry).
type Registry = registry.Registry

// RegistryOption configures a Registry.
type RegistryOption = registry.Option

// NewRegistry returns an empty registry.
func NewRegistry(logger *zap.Logger, opts ...RegistryOption) *Registry {
	return registry.NewRegistry(logger, opts...)
}

// WithHubID names this hub instance in the presence Directory so a
// fleet can tell which hub owns a peer.
func WithHubID(id string) RegistryOption { return registry.WithHubID(id) }

// WithDirectory sets the presence backend. Default is an in-memory
// directory, correct for a single hub.
func WithDirectory(dir Directory) RegistryOption { return registry.WithDirectory(dir) }

// WithMeterProvider sets the OTel meter provider for tunnel metrics.
// Default: the global provider.
func WithMeterProvider(mp metric.MeterProvider) RegistryOption {
	return registry.WithMeterProvider(mp)
}

// ErrPeerDetached is returned by the per-peer RoundTripper when no
// tunnel is attached. Applications use errors.Is to map it onto
// their own "not reachable" error shape.
var ErrPeerDetached = registry.ErrPeerDetached

// Event is one attach or detach, as delivered by Registry.Watch.
type Event = registry.Event

// EventKind says whether an Event is an attach or a detach.
type EventKind = registry.EventKind

// The event kinds.
const (
	EventAttached = registry.EventAttached
	EventDetached = registry.EventDetached
)

// TunnelInfo is the live view of one attached tunnel.
type TunnelInfo = registry.TunnelInfo

// Directory records peer presence — which peer is attached to which
// hub, since when. In-memory by default; SQL-backed for a fleet.
type Directory = directory.Directory

// PeerRecord is one peer's presence entry in a Directory.
type PeerRecord = directory.PeerRecord
