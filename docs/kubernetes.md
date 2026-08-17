[🌀 holt](../README.md) · [Docs](README.md) · **Kubernetes**

# Kubernetes

*Deploy the hub with the Helm chart.*

The chart ships as an OCI artifact next to the images:

```sh
helm install holt oci://ghcr.io/openotters/charts/holt
```

Full values in [`charts/holt/values.yaml`](../charts/holt/values.yaml).

## Shape

The hub runs as a **single replica** by default: live tunnels are
process-local, and so is the state on its volume. Point it at a shared
PostgreSQL (see [below](#shared-presence-directory-postgresql)) and
presence, the denylist, and the signing secret all move there, so
several hubs behave as one fleet: any hub verifies any hub's tokens.

Three services: the **tunnel** listener is a `LoadBalancer` (peers dial
it from outside the cluster), while **admin** and **proxy** stay
`ClusterIP` (private by default).

## Advertised address

The pod binds `0.0.0.0`, which peers cannot dial, so tokens minted in
the cluster must advertise the reachable tunnel URL. The scheme picks
the peer transport: `wss://holt.example.com` dials TLS under the
WebSocket (verified with the system roots, so it works through an
ingress, a LoadBalancer with TLS, or Cloudflare), while
`ws://192.168.8.193:7200` dials plaintext straight to a bare
LoadBalancer IP (`https`/`http` are accepted as aliases).

```yaml
hub:
  advertiseAddr: "wss://holt.example.com"
```

With the tunnel ingress enabled you can skip this: the chart advertises
`wss://<ingress.tunnel.host>` automatically (see
[Ingresses](#ingresses)).

The tunnel listener itself is plaintext, like the admin and proxy
listeners. Transport encryption is the deployment's job: put a TLS edge
(ingress, LoadBalancer with TLS, or a service mesh) in front of the hub
and advertise its `wss://` URL.

## Persistence

`persistence.enabled` (default true) binds a PVC for the hub state.
Losing it invalidates every join token already handed out. The pod runs
non-root; `fsGroup` makes the volume writable.

**With a shared PostgreSQL you can turn it off.** Presence, the
denylist, and the signing secret all live in the database then, and
nothing else on the volume outlives a restart, so:

```yaml
persistence:
  enabled: false          # emptyDir; the database is the state
postgres:
  cnpg:
    enabled: true
```

Identity is the piece that used to make this impossible: a hub with an
`emptyDir` minted a new secret on every restart, invalidating every
token. Reading it from the database fixes that, and is also what lets
you scale past one replica.

## Ingresses

All three listeners can be exposed through an ingress; all are
**disabled by default**. Admin and proxy should stay that way unless an
authenticating layer sits in front: the admin API mints tokens and can
kill/block peers, the proxy reaches every peer's service, and neither
has built-in auth. The intended exposure is behind a zero trust proxy
(Cloudflare Tunnel + Access) or an authenticating ingress.

The tunnel ingress is different: that listener is JWT-authenticated and
exists to be dialed from outside, so exposing it is the point. The
tunnel is a WebSocket, so any edge that forwards plain HTTP/1.1
upgrades carries it, Cloudflare public hostnames included; no HTTP/2
origin setting is needed (on the cloudflare-tunnel ingress class, do
NOT set the `http2-origin` annotation, an HTTP/2 origin connection
would break the upgrade).

```yaml
ingress:
  admin:
    enabled: true
    host: holt.example.com
  proxy:
    enabled: true
    host: peers.example.com
  tunnel:
    enabled: true
    host: holt-tunnel.example.com
```

Each listener also takes a `path` (default `/`, pathType `Prefix`), so
the three can share one hostname split by path (the hub accepts the
tunnel upgrade on any path).

Enabling the tunnel ingress auto-advertises `wss://<its host>` (plus
its non-root path) to peers when `hub.advertiseAddr` is empty; an
explicit `hub.advertiseAddr` always wins. Enabling the admin ingress
auto-adds its host to the DNS-rebinding host guard, so the console
keeps working while staying safe. See [Security](security.md).

## Health probes

Liveness and readiness probe `GET /healthz` on the admin port, never the
tunnel port (it has no health route, so a bare probe there just logs a
spurious request). Override either with `livenessProbe` /
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

## Subdomain routing

By default the proxy routes on the `x-tunnel-peer` header. Give every
peer its own hostname instead (so browsers, webhooks, and anything
else that only takes a URL can reach it) with:

```yaml
hub:
  proxyRouting: both          # header, subdomain, or both
  proxyDomain: peers.example.com

ingress:
  proxy:
    enabled: true
    host: "*.peers.example.com"   # wildcard record + wildcard cert
```

`subdomain` and `both` require `hub.proxyDomain`; the chart fails the
render otherwise. With a wildcard proxy ingress host the chart skips
the auto-derived `--external-url` (a wildcard is not a URL) and the
console shows each peer's own hostname in its Call command instead.

### Pick the domain depth before the certificate

A wildcard certificate matches exactly **one** label, so
`*.example.com` covers `alice.example.com` but never
`alice.peers.example.com`. Behind Cloudflare this bites immediately:
free Universal SSL covers the apex and first-level subdomains only, so
a `peers.` prefix needs [Advanced Certificate
Manager](https://developers.cloudflare.com/ssl/edge-certificates/advanced-certificate-manager/)
(paid) to add `*.peers.example.com`. Three ways out, cheapest first:

- **put peers at the first level** — `proxyDomain: example.com`, so
  `alice.example.com` is covered by the free wildcard. Costs nothing,
  but every peer id now shares the namespace with your other records.
- **give peers their own domain** — `proxyDomain: peers.example` with
  its own zone. First-level again, free certificate, and tunneled
  content no longer shares a registrable domain with your console
  (a peer cannot set a cookie on your admin hostname).
- **pay for the deeper wildcard** — keep `peers.example.com` and add
  the certificate through ACM.

Outside Cloudflare this is less painful: cert-manager with a DNS-01
solver issues `*.peers.example.com` from Let's Encrypt without a
paid tier.

## Metrics

```yaml
metrics:
  enabled: true
  serviceMonitor:
    enabled: true
    labels:
      release: kube-prometheus-stack   # so your Prometheus selects it
```

`metrics.enabled` opens `/metrics` on `metrics.port` (7203) and adds it
to the private service; the `ServiceMonitor` needs the Prometheus
Operator CRD.

Add `metrics.dashboard.enabled` for the bundled Grafana dashboard,
either as a `GrafanaDashboard` for grafana-operator
(`mode: operator`) or as a sidecar-labelled ConfigMap
(`mode: sidecar`, the default). See
[Observability](observability.md#grafana-dashboard) for the panels and
the rest of the knobs.

## Other knobs

- `hub.externalURL`: public proxy URL shown in the console's Call
  command; auto-derived from the proxy ingress host when set.
- `hub.maxConns`: cap concurrent tunnel connections.
- `hub.trafficBuffer`: recent proxied requests kept in memory to seed
  the console's traffic view (100; `0` keeps none). Nothing is written
  anywhere and the window dies with the pod.
- `hub.tokenTTL`, `hub.logLevel`, `resources`, `nodeSelector`,
  `tolerations`, `affinity`: as usual.

---

[← Security](security.md)  ·  [Docs home](README.md)  ·  [Observability →](observability.md)
