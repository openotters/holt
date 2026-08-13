[🌀 holt](../README.md) · [Docs](README.md) · **Kubernetes**

# Kubernetes

*Deploy the hub with the Helm chart.*

The chart ships as an OCI artifact next to the images:

```sh
helm install holt oci://ghcr.io/openotters/charts/holt
```

Full values in [`charts/holt/values.yaml`](../charts/holt/values.yaml).

## Shape

The hub runs as a **single replica** on purpose: the JWT secret,
blocklist, and live tunnels are process-local. The tunnel-presence
directory can move to a shared PostgreSQL (see
[below](#shared-presence-directory-postgresql)) so several hub releases
see each other's peers; each release still runs one replica.

Three services: the **tunnel** listener is a `LoadBalancer` (peers dial
it from outside the cluster), while **admin** and **proxy** stay
`ClusterIP` (private by default).

## Advertised address

The pod binds `0.0.0.0`, which peers cannot dial, so tokens minted in
the cluster must advertise the reachable tunnel URL. The scheme picks
the peer transport: `https://holt.example.com` dials standard TLS
(verified with the system roots, so it works through an ingress, a
LoadBalancer with TLS, or Cloudflare), while `http://192.168.8.193:7000`
dials plaintext h2c straight to a bare LoadBalancer IP.

```yaml
hub:
  advertiseAddr: "https://holt.example.com"
```

The tunnel listener itself is plaintext h2c, like the admin and proxy
listeners. Transport encryption is the deployment's job: put a TLS edge
(ingress, LoadBalancer with TLS, or a service mesh) in front of the hub
and advertise its `https://` URL.

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

Liveness and readiness probe `GET /healthz` on the admin port, never the
tunnel port (it speaks h2c and has no health route, so a bare probe there
just logs a spurious dial). Override either with `livenessProbe` /
`readinessProbe`.

## Shared presence directory (PostgreSQL)

By default presence lives in the hub's local SQLite state. The
`postgres` values move it to a shared PostgreSQL — the hub then starts
with `--directory-dsn` (via the `HOLT_DIRECTORY_DSN` env var). Three
mutually exclusive sources:

```yaml
postgres:
  # 1. Inline DSN (dev only; the password lands in the values).
  dsn: "postgres://user:pass@db:5432/holt"

  # 2. A key of an existing Secret, e.g. one your operator generates.
  existingSecret:
    name: my-db-app
    key: uri

  # 3. Provision a CloudNativePG Cluster. Needs the CNPG operator
  #    (postgresql.cnpg.io CRDs, https://cloudnative-pg.io).
  cnpg:
    enabled: true
    instances: 1
    storage:
      size: 1Gi
```

With `cnpg.enabled` the chart creates a `Cluster` named
`<fullname>-db`; the operator bootstraps the `app` database and a
`<fullname>-db-app` Secret whose `uri` key the deployment consumes.
Tune the rest of the Cluster spec (backups, parameters, image) through
`postgres.cnpg.extraSpec`, merged verbatim.

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
