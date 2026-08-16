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
`--metrics` (on `--metrics-addr`, default `127.0.0.1:7203`):

| Metric | Type | Labels |
|--------|------|--------|
| `holt_tunnels_active` | gauge | |
| `holt_tunnels_attaches_total` | counter | |
| `holt_tunnels_detaches_total` | counter | `reason` |
| `holt_tunnels_rejected_total` | counter | `reason` (unauthorized, blocked, invalid-peer-name) |
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

Most of these belong to the library, not to the CLI: the tunnel
lifecycle comes from `pkg/registry` and the data plane from
`pkg/revproxy`, so a program embedding them reports the same series
(and works with the dashboard below) without writing any
instrumentation. Only the reject counter and the build info are the
CLI's own.

### The peer side

A peer keeps its own instruments, which answer a question the hub
cannot: a hub only sees the attempts that reached it, while the peer
sees the ones that failed and how long each tunnel lasted before it
had to redial. `holt expose --metrics` serves them (default
`127.0.0.1:7210`):

| Metric | Type | Labels |
|--------|------|--------|
| `holt_peer_attached` | gauge | 1 while a tunnel is up |
| `holt_peer_attaches_total` | counter | |
| `holt_peer_attach_failures_total` | counter | `reason` (dial, handshake) |
| `holt_peer_session_duration_seconds` | histogram | how long each tunnel lasted |

A peer that flaps shows it here as a rising failure count and a
session histogram full of short sessions, while the hub only sees
tunnels coming and going.

The console's connection status menu links to the `/metrics` endpoint
when it is enabled.

## Kubernetes

The Helm chart wires this up with `metrics.enabled` and an optional
Prometheus Operator `ServiceMonitor` (`metrics.serviceMonitor.enabled`).
See [Kubernetes](kubernetes.md).

## Grafana dashboard

The chart ships a dashboard for exactly these metrics, off by default:

```yaml
metrics:
  enabled: true
  serviceMonitor:
    enabled: true
  dashboard:
    enabled: true
    mode: operator        # or sidecar
    folder: holt
    instanceSelector:     # operator mode: which Grafana gets it
      matchLabels:
        dashboards: grafana
```

Two ways in, because deployments differ:

- **`operator`** emits a `GrafanaDashboard`
  (`grafana.integreatly.org/v1beta1`) with the JSON inlined, for
  [grafana-operator](https://grafana.github.io/grafana-operator/) v5.
  `instanceSelector` picks the Grafana instances,
  `allowCrossNamespaceImport` lets one in another namespace take it.
- **`sidecar`** writes a ConfigMap labelled `grafana_dashboard: "1"`
  (override with `sidecarLabel` / `sidecarLabelValue`) and annotated
  with the folder, which is how kube-prometheus-stack's Grafana
  imports dashboards. No CRD required.

What it shows: attached peers now and over time, attach and detach
rates with the **reason** each tunnel dropped (`superseded` means a
peer reattached, `connection-lost` is the network), rejected attaches
by reason, proxied request rate coloured by status class, p50/p90/p99
latency on one axis, routing errors, and in-flight requests. A `job`
variable scopes it to one hub when several report to the same
Prometheus.

A last row covers what the **peers** report about themselves, when
they run with `--metrics`: how many say they are attached, the
attempts that failed before becoming a tunnel, and how long sessions
last. It is separated from the rest because it comes from other
processes: a peer that never reaches the hub appears there and
nowhere else.

The JSON lives at
[`charts/holt/dashboards/holt.json`](../charts/holt/dashboards/holt.json)
if you would rather import it by hand.

## As a library

Point the hub at your own providers:

```go
registry.NewRegistry(log, registry.WithMeterProvider(mp)) // pkg/registry
holt.NewTunnel(":7200", holt.WithHandlerOptions(holt.WithTracerProvider(tp)))
```

---

[← Kubernetes](kubernetes.md)  ·  [Docs home](README.md)  ·  [Library →](library.md)
