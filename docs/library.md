[🌀 holt](../README.md) · [Docs](README.md) · **Library**

# Library

*Embed the hub and dial halves in your own Go program.*

The `holt` CLI is one opinionated packaging (JWT auth, WebSocket
transport, SQLite state). The library lets you bring your own auth and
middleware. See [How it works](architecture.md) for the moving parts.

Peer side:

```go
dial.Run(ctx, dial.Options{
    URL:     "wss://holt.example.com",  // ws for plaintext; http/https accepted as aliases
    Header:  http.Header{"Authorization": {"Bearer " + token}},
    Handler: myHandler, Version: build.Version, Logger: log,
})
```

Hub side — `hub.NewServer` is `dial.Run`'s counterpart, the whole
server in one call:

```go
srv := hub.NewServer(
    hub.WithLogger(log),
    hub.WithTunnel(hub.NewTunnel(":7000",     // where peers attach
        hub.WithAuthBearer(peerForToken),     // token → peer id, 401s the rest
    )),
    hub.WithProxy(hub.NewProxy(":7002")),     // reach peers: x-tunnel-peer header
)

err := srv.Run(ctx) // binds (fail fast), serves, blocks; cancel to drain

// meanwhile, anywhere in the hub process:
client := &http.Client{Transport: srv.Registry().RoundTripper(peerID)}
```

Zero configuration works too: `hub.NewServer().Run(ctx)` serves a
tunnel on `127.0.0.1:7000` and a proxy on `127.0.0.1:7002` with the
**development identity** — peers name themselves with the
`x-holt-peer` header (or get a generated name), nothing verifies the
claim. It is loopback-only by construction: a tunnel bound anywhere
another machine could reach it refuses to start without a real
identity.

## Endpoints and identity

`NewTunnel` and `NewProxy` declare the two endpoints; both take
`WithListener` (bring your own — a `tls.NewListener` for wss, a
systemd socket) and `WithMiddleware` (any `func(http.Handler)
http.Handler`, first-listed outermost).

The tunnel's identity decides what keys the registry, and it must
come from something verified — never from what the peer asserts:

```go
// Bearer tokens: one func from token to peer id does middleware and
// identity both.
hub.NewTunnel(":7000", hub.WithAuthBearer(peerForToken))

// Any other scheme: middleware stamps the context, identity reads it
// back (a client-cert CN here; see examples/transport-tls).
hub.NewTunnel("", hub.WithListener(tlsLis),
    hub.WithMiddleware(certIdentity), hub.WithIdentity(cnFromCtx))

// Inner TLS and tracing pass through to the attach handler:
hub.NewTunnel(":7000", hub.WithHandlerOptions(hub.WithPeerTLS(cfg)))
```

The proxy routes on the `x-tunnel-peer` header by default;
`hub.WithRouting(proxy.RoutingBoth, "peers.example.com")` adds
per-peer hostnames, and `hub.WithErrorHook` observes requests that
could not be proxied. A request that names no peer, or names one that
is not attached, never reaches a backend: it gets a bare page that
says nothing about the hub (400 and 404 respectively, never a 502).

## Underneath: your own router

`NewServer` assembles public pieces you can also mount yourself, for
an application with its own HTTP server and lifecycle:

```go
registry := hub.NewRegistry(log)
// NewHandler is an http.Handler that accepts the WebSocket upgrade;
// wrap it in your auth middleware, which sees the upgrade request's
// headers and stamps the identity on its context.
mux.Handle("/attach", authMiddleware(hub.NewHandler(registry, identityFromCtx, log)))
// hub/proxy is the data plane; validate the routing pair at boot with
// routing.Validate(domain).
mux.Handle("/", proxy.New(registry))
```

## Operating the hub

The `Registry` (from `srv.Registry()`, or your own `NewRegistry`) is
the operational surface over live tunnels:

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

Point the hub at your own OTel providers:

```go
hub.NewRegistry(log, hub.WithMeterProvider(mp))
hub.NewHandler(reg, id, log, hub.WithTracerProvider(tp))
```

See [Observability](observability.md) for the instruments and the
CLI's built-in Prometheus endpoint.

---

[← Observability](observability.md)  ·  [Docs home](README.md)  ·  [Examples →](examples.md)
