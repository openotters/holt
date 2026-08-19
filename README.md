<div align="center">

# 🌀 holt

**Reverse HTTP tunnels for services that can only dial out.**

[![Go Reference](https://pkg.go.dev/badge/github.com/openotters/holt.svg)](https://pkg.go.dev/github.com/openotters/holt)
[![Go Report Card](https://goreportcard.com/badge/github.com/openotters/holt)](https://goreportcard.com/report/github.com/openotters/holt)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE.md)
[![Status: alpha](https://img.shields.io/badge/status-alpha-orange.svg)](#)

</div>

A *holt* is an otter's den: a burrow in the riverbank, reachable only
through the underwater tunnel its owner dug. Same idea here. A **peer**
that cannot accept inbound connections (NAT, locked-down container,
field device) dials out to a **hub**, then serves an ordinary
`http.Handler` back through the connection it opened. The hub gets an
`http.RoundTripper` per peer, and presence for free. No listener, no
inbound port, nothing published on the peer.

holt is a **Go library first**; the `holt` CLI is one opinionated
packaging of it.

> ⚠️ Alpha, extracted from [openotters](https://github.com/openotters/openotters),
> where it is the daemon-to-agent channel. The wire protocol may still change.

<div align="center">
  <img src="docs/demo.gif" alt="holt in thirty seconds: hub up, service exposed, reached through the tunnel" width="720" />
</div>

## The library

Two constructors, both at the module root:

```go
// The peer: attaches to the hub, serves the handler through the
// tunnel, redials with backoff. Cancel ctx to stop.
c := holt.NewClient("wss://holt.example.com", myHandler,
	holt.WithBearerToken(token))
err := c.Run(ctx)
```

```go
// The hub: a tunnel listener peers attach to, an optional proxy to
// reach them from outside.
srv := holt.NewServer(
	holt.WithTunnel(holt.NewTunnel(":7200", holt.WithAuthBearer(peerForToken))),
	holt.WithProxy(holt.NewProxy(":7202")),
)
go srv.Run(ctx)

// Anywhere in the hub process, a peer is an ordinary HTTP backend:
client := &http.Client{Transport: srv.Registry().RoundTripper(peerID)}
```

Bring your own auth, middleware, listeners and storage — everything
the CLI adds (JWT identity, SQLite/PostgreSQL state, the console) is
built on this surface. See [Library](docs/library.md) and
[How it works](docs/architecture.md).

## The CLI

One binary for hub, peer, and operations
([all install methods](docs/install.md)):

```sh
brew install openotters/tap/holt   # or: go install github.com/openotters/holt/cmd/holt@latest

holt hub --ui &                         # hub + web console (127.0.0.1:7201)
holt expose localhost:3000 --peer web   # enrolls itself, serves the tunnel
curl -H 'x-tunnel-peer: web' http://127.0.0.1:7202/
```

Peers authenticate with a JWT and attach over a **WebSocket**, so the
tunnel passes through Cloudflare, ingresses and access proxies. TLS is
your edge's job: advertise its `wss://` URL. With a domain, each peer
gets its own hostname — for a browser, a webhook, an OAuth callback:

```sh
holt hub --advertise-addr wss://holt.example.com \
  --proxy-routing both --proxy-domain example.com
holt expose localhost:3000 --peer checkout
# https://checkout.example.com/ now reaches the service
```

Operate with `holt ls`, `holt kill web`, `holt block web`. On
Kubernetes, `helm install holt oci://ghcr.io/openotters/charts/holt`;
several hubs share one PostgreSQL for presence, denylist and signing
identity ([Kubernetes](docs/kubernetes.md)).

## The console

`holt hub --ui` serves a web console. **Capture endpoints** are
throwaway addresses that accept any call and show it live — point a
webhook or an OAuth redirect at one and inspect what arrives, payload
included, without exposing a real service:

<div align="center">
  <img src="docs/console-capture.png" alt="a Stripe webhook opened in the capture inspector" width="760" />
</div>

The live tunnel roster (kill, block, call any peer), and per-peer
traffic with payloads — any request is one click from a curl that
replays it:

<p align="center">
  <img src="docs/console-tunnels.png" alt="live tunnels with attach/detach activity" width="378" />
  <img src="docs/console-traffic.png" alt="per-peer live traffic with statuses and timings" width="378" />
</p>

See [Web console](docs/console.md).

Prometheus metrics and a Grafana dashboard come with it
([Observability](docs/observability.md)).

## Where holt fits

frp, ngrok and inlets do more, at a bigger scale: TCP/UDP, load
balancing, teams, hosted service. holt is HTTP(S) and gRPC through
one hub on your own infra — for embedding tunnels in a Go program,
and for the simple case where your traffic should stay yours.

## Documentation

Full docs live in [`docs/`](docs/README.md):

- **Get started**: [Install](docs/install.md) · [How it works](docs/architecture.md)
- **Use holt**: [CLI](docs/cli.md) · [Web console](docs/console.md)
- **Operate**: [Security](docs/security.md) · [Kubernetes](docs/kubernetes.md) · [Observability](docs/observability.md)
- **Build with holt**: [Library](docs/library.md) · [Examples](docs/examples.md) · [Development](docs/development.md)

## License

[MIT](LICENSE.md)
