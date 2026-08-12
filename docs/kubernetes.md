[🌀 holt](../README.md) · [Docs](README.md) · **Kubernetes**

# Kubernetes

*Deploy the hub with the Helm chart.*

The chart ships as an OCI artifact next to the images:

```sh
helm install holt oci://ghcr.io/openotters/charts/holt
```

Full values in [`charts/holt/values.yaml`](../charts/holt/values.yaml).

## Shape

The hub runs as a **single replica** on purpose: its state (cert, JWT
secret, blocklist, presence) lives in a local SQLite database. Fleets
with several hubs use the [library](library.md) with a shared PostgreSQL
directory, not this chart.

Three services: the **tunnel** listener is a `LoadBalancer` (peers dial
it from outside the cluster), while **admin** and **proxy** stay
`ClusterIP` (private by default).

## Advertised address

The pod binds `0.0.0.0`, which peers cannot dial, so tokens minted in
the cluster must advertise the reachable tunnel address. Set it to the
tunnel Service's LoadBalancer IP and port:

```yaml
hub:
  advertiseAddr: "192.168.8.193:7000"
```

## Persistence

`persistence.enabled` (default true) binds a PVC for the hub state.
Losing it invalidates every join token already handed out. The pod runs
non-root; `fsGroup` makes the volume writable.

## Ingresses

Both ingresses are **disabled by default** and should stay that way
unless an authenticating layer sits in front. The admin API mints tokens
and can kill/block peers; the proxy reaches every peer's service.
Neither has built-in auth. The intended exposure is behind a zero trust
proxy (Cloudflare Tunnel + Access) or an authenticating ingress.

```yaml
ingress:
  admin:
    enabled: true
    host: holt.example.com
  proxy:
    enabled: true
    host: peers.example.com
```

Enabling the admin ingress auto-adds its host to the DNS-rebinding host
guard, so the console keeps working while staying safe. See
[Security](security.md).

## Health probes

Liveness and readiness probe `GET /healthz` on the admin port (plaintext),
never the TLS tunnel port (a TCP poke of a TLS listener logs an aborted
handshake). Override either with `livenessProbe` / `readinessProbe`.

## Metrics

```yaml
metrics:
  enabled: true
  serviceMonitor:
    enabled: true
    labels:
      release: kube-prometheus-stack   # so your Prometheus selects it
```

`metrics.enabled` opens `/metrics` on `metrics.port` (7003) and adds it
to the private service; the `ServiceMonitor` needs the Prometheus
Operator CRD. See [Observability](observability.md) for the metrics.

## Other knobs

- `hub.externalURL`: public proxy URL shown in the console's Call
  command; auto-derived from the proxy ingress host when set.
- `hub.maxConns`: cap concurrent tunnel connections.
- `hub.tokenTTL`, `hub.logLevel`, `resources`, `nodeSelector`,
  `tolerations`, `affinity`: as usual.

---

[← Security](security.md)  ·  [Docs home](README.md)  ·  [Observability →](observability.md)
