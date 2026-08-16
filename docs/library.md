[🌀 holt](../README.md) · [Docs](README.md) · **Library**

# Library

*Embed the hub and dial halves in your own Go program.*

The `holt` CLI is one opinionated packaging (JWT auth, WebSocket
transport, SQLite state). The library lets you bring your own auth and
middleware. See [How it works](architecture.md) for the moving parts.

Both halves live at the root of the module: `holt.NewClient` for the
peer, `holt.NewServer` for the hub.

Peer side:

```go
c := holt.NewClient("wss://holt.example.com", myHandler,
    holt.WithBearerToken(token),
    holt.WithVersion(build.Version),
    holt.WithLogger(log),
)

err := c.Run(ctx) // attaches, serves, redials with backoff; cancel to stop
```

Hub side — `holt.NewServer` is `NewClient`'s counterpart, the whole
server in one call:

```go
srv := holt.NewServer(
    holt.WithLogger(log),
    holt.WithTunnel(holt.NewTunnel(":7000",     // where peers attach
        holt.WithAuthBearer(peerForToken),      // token → peer id, 401s the rest
    )),
    holt.WithProxy(holt.NewProxy(":7002")),     // reach peers: x-tunnel-peer header
)

err := srv.Run(ctx) // binds (fail fast), serves, blocks; cancel to drain

// meanwhile, anywhere in the hub process:
client := &http.Client{Transport: srv.Registry().RoundTripper(peerID)}
```

Zero configuration works too: `holt.NewServer().Run(ctx)` serves a
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
holt.NewTunnel(":7000", holt.WithAuthBearer(peerForToken))

// Any other scheme: middleware stamps the context, identity reads it
// back (a client-cert CN, a session cookie, any verified source).
holt.NewTunnel("", holt.WithListener(tlsLis),
    holt.WithMiddleware(certIdentity), holt.WithIdentity(cnFromCtx))

// Inner TLS and tracing pass through to the attach handler:
holt.NewTunnel(":7000", holt.WithHandlerOptions(holt.WithPeerTLS(cfg)))
```

The proxy routes on the `x-tunnel-peer` header by default;
`holt.WithRouting(holt.RoutingBoth, "peers.example.com")` adds
per-peer hostnames, and `holt.WithErrorHook` observes requests that
could not be proxied. A request that names no peer, or names one that
is not attached, never reaches a backend: it gets a bare page that
says nothing about the hub (400 and 404 respectively, never a 502).

## Underneath: your own router

The root package configures the two constructors; the pieces
`NewServer` assembles are public under `pkg/` for an application with
its own HTTP server and lifecycle (`pkg/dial` is the same escape
hatch on the client side):

```go
reg := registry.NewRegistry(log)                  // pkg/registry
// attach.NewHandler is an http.Handler that accepts the WebSocket
// upgrade; wrap it in your auth middleware, which sees the upgrade
// request's headers and stamps the identity on its context.
mux.Handle("/attach", authMiddleware(attach.NewHandler(reg, identityFromCtx, log)))
// pkg/revproxy is the data plane.
mux.Handle("/", revproxy.New(reg))
```

## Operating the hub

The `Registry` (from `srv.Registry()`, or your own
`registry.NewRegistry`) is the operational surface over live tunnels
— it lives in `pkg/registry`:

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
attached, and where?". The SQL directory supports SQLite and
PostgreSQL and imports only `database/sql` (you bring the driver):

```go
db, _ := sql.Open("sqlite", "file:presence.db")   // or pgx/stdlib
dir := sqlite.New(db)          // pkg/directory/sqlite; postgres flavour next door
_ = dir.Migrate(ctx)

reg := registry.NewRegistry(log,
    registry.WithHubID("hub-eu-1"),  // stable per-instance id, recorded in rows
    registry.WithDirectory(dir))
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
registry.NewRegistry(log, registry.WithMeterProvider(mp)) // pkg/registry
holt.NewTunnel(":7000", holt.WithHandlerOptions(holt.WithTracerProvider(tp)))
```

See [Observability](observability.md) for the instruments and the
CLI's built-in Prometheus endpoint.

---

[← Observability](observability.md)  ·  [Docs home](README.md)  ·  [Examples →](examples.md)
