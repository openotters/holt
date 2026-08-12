<div align="center">

# 🌀 holt

**Reverse HTTP tunnels for services that can only dial out.**

[![Go Reference](https://pkg.go.dev/badge/github.com/openotters/holt.svg)](https://pkg.go.dev/github.com/openotters/holt)
[![Go Report Card](https://goreportcard.com/badge/github.com/openotters/holt)](https://goreportcard.com/report/github.com/openotters/holt)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE.md)
[![Status: experimental](https://img.shields.io/badge/status-experimental-orange.svg)](#)

</div>

A *holt* is an otter's den: a burrow in the riverbank, reachable only
through the underwater tunnel its owner dug. Same idea here. A **peer**
that cannot accept inbound connections (behind NAT, in a locked down
container, on a field device) dials out to a **hub**, then serves an
ordinary `http.Handler` back through the connection it opened. The hub
gets an `http.RoundTripper` per peer, and a presence signal for free.

No listener, no inbound port, no published container port on the peer.

> ⚠️ Alpha, extracted from [openotters](https://github.com/openotters/openotters)
> where it is the sole daemon-to-agent-runtime channel. The wire protocol
> may still change.

<div align="center">
  <img src="docs/console.png" alt="holt web console" width="760" />
  <br />
  <sub>The built-in web console (<code>holt hub --ui</code>). See <a href="docs/console.md">Web console</a>.</sub>
</div>

## Quickstart (local)

Install it (see [all install methods](docs/install.md)):

```sh
brew install openotters/tap/holt      # or: go install github.com/openotters/holt/cmd/holt@latest
```

Expose a local service through a hub in three commands:

```sh
holt hub --ui &                              # run a hub (web console on 127.0.0.1:7001)
holt enroll web                              # mint a join token for a peer named "web"
holt expose localhost:3000 --token <paste>   # on the machine running the service
```

Then reach the peer *through* the hub:

```sh
curl -H 'x-tunnel-peer: web' http://127.0.0.1:7002/
```

The token bundles the peer's JWT and the hub's tunnel URL. Peers
authenticate with the JWT, and the URL's scheme picks the transport: an
`https://` URL is encrypted by a TLS edge in front of the hub (an
ingress, LoadBalancer, or Cloudflare), while `http://` dials plaintext
h2c for local use. The hub itself never manages a certificate.

Behind a TLS edge, advertise the public URL so peers dial it over TLS:

```sh
holt hub --advertise-addr https://holt.example.com &   # hub is h2c; your edge terminates TLS
holt enroll web                                        # token now carries the https URL
```

See [Security](docs/security.md) for exposing a hub safely.

## Documentation

Full docs live in [`docs/`](docs/README.md):

- **Get started**: [Install](docs/install.md) · [How it works](docs/architecture.md)
- **Use holt**: [CLI](docs/cli.md) · [Web console](docs/console.md)
- **Operate**: [Security](docs/security.md) · [Kubernetes](docs/kubernetes.md) · [Observability](docs/observability.md)
- **Build with holt**: [Library](docs/library.md) · [Examples](docs/examples.md) · [Development](docs/development.md)

## License

[MIT](LICENSE.md)
