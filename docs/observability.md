[🌀 holt](../README.md) · [Docs](README.md) · **Observability**

# Observability

*Prometheus metrics and OpenTelemetry instrumentation.*

The hub records OTel instruments and a span per tunnel (`holt.tunnel`,
tagged with peer, version, detach reason), against the global OTel
providers by default, which are a no-op until you install an SDK:
instrumentation is always present, never mandatory, and costs nothing
when unconfigured.

## Prometheus metrics

The `holt` CLI can expose the instruments as Prometheus metrics with
`--metrics` (on `--metrics-addr`, default `127.0.0.1:7003`):

| Metric | Type | Labels |
|--------|------|--------|
| `holt_tunnels_active` | gauge | |
| `holt_tunnels_attaches_total` | counter | |
| `holt_tunnels_detaches_total` | counter | `reason` |
| `holt_tunnels_rejected_total` | counter | `reason` (unauthorized, blocked) |
| `holt_proxy_requests_total` | counter | `code` |
| `holt_proxy_request_duration_seconds` | histogram | `code` |
| `holt_proxy_inflight` | gauge | |
| `holt_proxy_errors_total` | counter | `reason` (no-header, not-attached, transport) |
| `holt_build_info` | gauge | `version`, `commit` |

The tunnel-lifecycle and build-info series show on a fresh hub (before
any traffic); the proxy and reject counters appear as they happen.
Labels are deliberately low-cardinality (code, reason), never per-peer.
The default Go and process collectors (`go_*`, `process_*`) are served
alongside them.

The console's connection status menu links to the `/metrics` endpoint
when it is enabled.

## Kubernetes

The Helm chart wires this up with `metrics.enabled` and an optional
Prometheus Operator `ServiceMonitor` (`metrics.serviceMonitor.enabled`).
See [Kubernetes](kubernetes.md).

## As a library

Point the hub at your own providers:

```go
hub.NewRegistry(log, hub.WithMeterProvider(mp))
hub.NewHandler(reg, id, log, hub.WithTracerProvider(tp))
```

---

[← Kubernetes](kubernetes.md)  ·  [Docs home](README.md)  ·  [Library →](library.md)
