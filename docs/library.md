[🌀 holt](../README.md) · [Docs](README.md) · **Library**

# Library

*Embed the hub and dial halves in your own Go program.*

The `holt` CLI is one opinionated packaging (JWT auth, h2c transport,
SQLite state). The library lets you bring your own auth and transport.
See [How it works](architecture.md) for the moving parts.

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

Point the hub at your own OTel providers:

```go
hub.NewRegistry(log, hub.WithMeterProvider(mp))
hub.NewHandler(reg, id, log, hub.WithTracerProvider(tp))
```

See [Observability](observability.md) for the instruments and the
CLI's built-in Prometheus endpoint.

---

[← Observability](observability.md)  ·  [Docs home](README.md)  ·  [Examples →](examples.md)
