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

## Contents

- [Install](#install)
- [Quickstart](#quickstart)
- [How it works](#how-it-works)
- [Use it as a library](#use-it-as-a-library)
- [CLI cheat sheet](#cli-cheat-sheet)
- [Web console](#web-console)
- [Securing an exposed hub](#securing-an-exposed-hub)
- [Operating the hub](#operating-the-hub)
- [Presence directory](#presence-directory)
- [Identity](#identity)
- [Liveness and lifecycle](#liveness-and-lifecycle)
- [Observability](#observability)
- [Examples](#examples)
- [Development](#development)
- [License](#license)

## Install

### Homebrew (macOS and Linux)

```sh
brew install openotters/tap/holt
```

### Binary

Grab a prebuilt binary for your OS and architecture (darwin/linux,
amd64/arm64) from the [releases page](https://github.com/openotters/holt/releases),
or install it with Go:

```sh
go install github.com/openotters/holt/cmd/holt@latest
```

### Docker

Multi-arch images (amd64/arm64) are published on ghcr:

```sh
docker run --rm ghcr.io/openotters/holt:latest --version

# run a hub (bind to 0.0.0.0 so the ports are reachable from outside
# the container). --tmpfs keeps state in memory for a quick try.
docker run --rm -p 7000:7000 -p 7001:7001 -p 7002:7002 --tmpfs /data \
  ghcr.io/openotters/holt:latest hub --state /data \
  --tunnel-addr 0.0.0.0:7000 --admin-addr 0.0.0.0:7001 --proxy-addr 0.0.0.0:7002
```

The image runs as a non-root user, so state on `--tmpfs` is lost on
restart (every join token with it). For durable state give it a `/data`
writable by uid `65532` (a host directory `chown`ed to it, or run with
`--user 0`); on Kubernetes the [Helm chart](#kubernetes-helm) handles
this with `fsGroup`.

### Kubernetes (Helm)

The chart ships as an OCI artifact next to the images:

```sh
helm install holt oci://ghcr.io/openotters/charts/holt
```

It runs the hub with persistent state, a LoadBalancer for the tunnel
listener, and the admin console and proxy kept private (ingresses off
by default); see [`charts/holt/values.yaml`](charts/holt/values.yaml)
and [Securing an exposed hub](#securing-an-exposed-hub).

### As a library

```sh
go get github.com/openotters/holt
```

## Quickstart

Expose a local service through a hub in four commands:

```sh
holt hub --ui &                      # run a hub (web console on 127.0.0.1:7001)
holt enroll web                      # mint a join token for a peer named "web"
holt expose localhost:3000 --token <paste>   # on the peer machine
```

Then reach the peer *through* the hub:

```sh
curl -H 'x-tunnel-peer: web' http://127.0.0.1:7002/
```

The token bundles the peer's JWT, the hub address, and the hub's
self-signed certificate to pin. The tunnel is TLS encrypted and both
sides are authenticated, out of the box.

## How it works

```
  peer (dials out)                         hub (public)
      │                                       │
      │-- Tunnel.Attach (bidi gRPC) --------->│   auth middleware → peer id
      │-- Hello ----------------------------->│
      │<----------------------------- Welcome │   registry.Attach(peer)
      │                                       │
      │  http2.Server.ServeConn(tunnel,       │   http2.Transport over tunnel:
      │    yourHandler)                       │   registry.RoundTripper(peer)
      │                                       │
      │◀======= HTTP request =================│   client.Do(req) → peer's handler
      │======== HTTP response ===============>│
```

Everything rides a single bidirectional gRPC stream. The stream carries
raw bytes (`TunnelFrame.data`, at most 32 KiB per frame); each side runs
a standard HTTP/2 endpoint over it, server on the peer, client on the
hub. Presence of the tunnel doubles as the peer's liveness signal.

The module has four parts:

- **`hub`**: server side. `NewRegistry` tracks live tunnels per peer;
  `NewHandler` is the `Tunnel.Attach` implementation you mount behind
  your auth middleware.
- **`dial`**: client side. `dial.Run` is a persistent attach loop that
  serves your `http.Handler` over the tunnel and redials with jittered
  backoff. It rides an existing `*grpc.ClientConn`, so it reuses
  whatever auth interceptors you already attached.
- **`hub/sqldir`**: a SQL-backed presence directory (SQLite or
  PostgreSQL) for sharing which peer is attached to which hub across a
  fleet.
- **root package**: the shared `Conn` (stream to `net.Conn` adapter),
  handshake, and `GoAway` vocabulary.

## Use it as a library

The `holt` CLI is one opinionated packaging (JWT auth, pinned TLS,
SQLite state). The library lets you bring your own auth and transport.

Peer side:

```go
cc, _ := grpc.NewClient(hubAddr, grpc.WithTransportCredentials(creds), authInterceptors...)
dial.Run(ctx, dial.Options{Conn: cc, Handler: myHandler, Version: build.Version, Logger: log})
```

Hub side:

```go
registry := hub.NewRegistry(log)
path, h := holtv1connect.NewTunnelHandler(
    hub.NewHandler(registry, identityFromJWT, log),
    connect.WithInterceptors(authInterceptor),
)
mux.Handle(path, h)

// later, anywhere in the hub process:
client := &http.Client{Transport: registry.RoundTripper(peerID)}
```

## CLI cheat sheet

```sh
holt hub                     # run the hub (tunnel, admin, proxy listeners)
holt hub --ui                # same, plus the web console on the admin port
holt enroll <peer>           # mint a join token for a peer
holt expose <addr> --token <t>  # expose a local HTTP service through the hub
holt ls                      # list live tunnels
holt kill <peer>             # disconnect a tunnel (the peer may reconnect)
holt block <peer>            # disconnect AND ban the peer id
holt unblock <peer>          # lift a block
holt renew                   # regenerate the hub cert (invalidates all tokens)
```

Run `holt <cmd> --help` for details. Hub state (certificate, JWT
secret, blocklist) lives in `~/.holt`.

Output is friendly by default: a welcome banner with the addresses,
readable logs, tables and checkmarks. For production, switch to the
classic structured logs with `--log-format json` (or the
`HOLT_LOG_FORMAT=json` environment variable). A first `Ctrl-C` drains
gracefully (peers get a `GoAway`, listeners finish in flight requests);
a second one forces the process to exit now.

## Web console

<p align="center">
  <img src="docs/console.png" alt="holt web console" width="640" />
</p>

`holt hub --ui` serves a small React console on the admin listener:

- the live tunnel list, updated over a server stream (no polling), with
  per-peer **Kill**, **Block**, and a **Call** button that shows the
  `curl` command to reach that peer through the proxy.
- an **enroll** form that mints a join token (shown in full, one click
  to copy).
- a **Danger zone** to renew the hub certificate. It hot-swaps the live
  cert and closes existing tunnels, so peers must re-enroll (the same
  effect as `holt renew`, without a restart).

Set `--external-url https://peers.example.com` when the proxy is
reachable at a public address; the console's Call command then shows
that URL too, and it appears in the startup banner.

## Securing an exposed hub

The tunnel listener is always TLS + JWT. The **admin** and **proxy**
listeners have no built-in auth and bind to `127.0.0.1` by default;
they are meant to stay on loopback or sit behind an authenticating
proxy (a zero trust tunnel, an auth ingress). When you do expose them:

- a **Host guard** on the console rejects requests whose `Host` is not
  loopback or an allow-listed name, defeating DNS-rebinding. Add your
  public hostname with `--allowed-host` (repeatable; `*` disables it).
- `--max-conns` caps concurrent tunnel connections.
- the [Helm chart](charts/holt) keeps both ingresses disabled by
  default and auto-adds the admin ingress host to the guard.

## Operating the hub

The `Registry` is the operational surface over live tunnels:

```go
registry.ListTunnels()             // every live tunnel on this hub
registry.Tunnel(peer)              // one, with attach time and peer version
registry.CountTunnels()            // how many are up
registry.StopTunnel(peer, reason)  // force one closed (GoAway to the peer)
registry.StopAllTunnels(reason)    // drain, e.g. on shutdown
registry.Attached(peer)            // is peer attached to THIS hub?
registry.Watch(ctx)                // stream attach/detach events
```

The same surface is exposed over the wire by the Admin gRPC service
(`holt ls`, `holt kill`, ...) and by the web console.

## Presence directory

`RoundTripper` needs the live HTTP/2 connection, which only the owning
hub holds, so live connections are always local. What is pluggable is
*presence*: which peer is attached, to which hub, since when. That is
the `Directory` interface, in-memory by default (a single hub needs
nothing else).

For a fleet, back it with SQL so any hub can answer "is peer X
attached, and where?". `hub/sqldir` supports SQLite and PostgreSQL and
imports only `database/sql` (you bring the driver):

```go
db, _ := sql.Open("sqlite", "file:presence.db")           // or pgx/stdlib
dir := sqldir.New(db, sqldir.SQLite); _ = dir.Migrate(ctx)

reg := hub.NewRegistry(log,
    hub.WithHubID("hub-eu-1"),   // stable per-instance id, recorded in rows
    hub.WithDirectory(dir))
_ = reg.ClearStale(ctx)          // drop rows this hub left after a crash

reg.LookupPeer(ctx, peer)        // fleet-wide: which hub owns it
reg.Peers(ctx)                   // fleet-wide roster
```

Cross-hub request forwarding (dialing a peer attached to another hub)
is left to the application; `LookupPeer` tells it which hub to forward
to.

## Identity

Peer identity is the application's job, never the handshake's. The hub
`Handler` takes an `Identity func(ctx) (peer string, err error)` that
reads whatever your middleware established: a JWT claim, an mTLS SAN, a
header. A second in-band source of truth could only ever agree, or be
an attack.

## Liveness and lifecycle

- Inner HTTP/2 PINGs (`ReadIdleTimeout`) detect a wedged peer end to
  end, through the tunnel, not just a dead TCP socket.
- A second `Attach` for the same peer **replaces** the first
  (`GoAway("superseded")`), so a crashed-and-redialed peer never waits
  out a keepalive timeout.
- Terminal `GoAway` reasons (`superseded`, `token-revoked`,
  `peer-stopping`) stop the peer redialing; everything else is retried
  with backoff.

## Observability

The hub emits OTel metrics (`holt.tunnels.active` / `.attaches` /
`.detaches`) and a span per tunnel (`holt.tunnel`, tagged with peer,
version, detach reason). It uses the global OTel providers by default,
which are a no-op until you install an SDK: instrumentation is always
present, never mandatory, and costs nothing when unconfigured.

```go
hub.NewRegistry(log, hub.WithMeterProvider(mp))
hub.NewHandler(reg, id, log, hub.WithTracerProvider(tp))
```

## Examples

Every example under [`examples/`](examples) is runnable with `go run`:

| Example         | What it shows                                              |
|-----------------|------------------------------------------------------------|
| `echo`          | Smallest end-to-end demo, hub and peer in one process      |
| `client-server` | Standalone hub and peer as two programs                    |
| `authenticated` | Bearer-token auth interceptors on both sides               |
| `transport-tls` | Mutual TLS on the outer gRPC hop                           |
| `encrypted`     | Plaintext outer hop, mutual TLS *inside* the tunnel        |
| `join-token`    | Token-based enrollment, like the CLI does it               |
| `grpc-tunnel`   | A peer serving gRPC (not just HTTP) through the tunnel     |

There is also [`cmd/starter-client`](cmd/starter-client), a minimal
peer meant to be copied as the starting point of your own.

## Development

Everything goes through [Task](https://taskfile.dev):

```sh
task                # list all tasks
task build          # build ./bin/holt
task dev            # run a hub with the web console from source
task test:race      # tests with the race detector
task lint           # golangci-lint (same config as CI)
task gen            # regenerate protobuf stubs (Go + TypeScript)
task ui:build       # rebuild the embedded web console
task check          # full local gate, same as CI
```

The web console is a small React app under [`ui/`](ui), embedded into
the binary with `go:embed`.

## License

[MIT](LICENSE.md)
