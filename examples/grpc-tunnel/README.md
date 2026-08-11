# grpc-tunnel — call a peer's gRPC service with grpcurl

Two standalone binaries. Each peer serves a real **gRPC service** (the
standard health service, plus reflection) over its tunnel and listens
on nothing. The hub exposes an operator gRPC endpoint that
reverse-proxies each call down the right peer's tunnel — so you can
drive a listenerless peer's gRPC service with plain `grpcurl`.

- **`client/`** — a peer. Runs a `*grpc.Server` (health + reflection),
  served over the tunnel (`*grpc.Server` is an `http.Handler`). Attaches
  under an `--id` (`client-1`, `client-2`, …).
- **`server/`** — the hub. Accepts tunnels on one port; on another,
  exposes an h2c gRPC endpoint that routes each call to the peer named
  in an `x-tunnel-peer` header and proxies the whole exchange —
  reflection included — through the tunnel.

## Run it

```bash
go run ./examples/grpc-tunnel/server

go run ./examples/grpc-tunnel/client --id client-1
go run ./examples/grpc-tunnel/client --id client-2
```

Then reach a peer's gRPC service through the hub — no `.proto` files
needed, thanks to reflection:

```bash
# list the peer's services (reflection, over the tunnel)
grpcurl -plaintext -H 'x-tunnel-peer: client-1' localhost:7501 list
#   grpc.health.v1.Health
#   grpc.reflection.v1.ServerReflection

# call a method on client-1
grpcurl -plaintext -H 'x-tunnel-peer: client-1' localhost:7501 \
        grpc.health.v1.Health/Check
#   { "status": "SERVING" }

# same call, routed to a different peer
grpcurl -plaintext -H 'x-tunnel-peer: client-2' localhost:7501 \
        grpc.health.v1.Health/Check

# describe a method via reflection
grpcurl -plaintext -H 'x-tunnel-peer: client-1' localhost:7501 \
        describe grpc.health.v1.Health.Check

# an unattached peer → clean UNAVAILABLE
grpcurl -plaintext -H 'x-tunnel-peer: nope' localhost:7501 \
        grpc.health.v1.Health/Check
#   Unavailable: peer "nope" is not attached
```

## How the routing works

The operator endpoint is an `httputil.ReverseProxy` whose transport is
per-peer: it reads the `x-tunnel-peer` header, strips it, and forwards
the request through `registry.RoundTripper(peer)` — the tunnel to that
peer. `FlushInterval: -1` streams every frame, which gRPC (and the
reflection bidi stream) require.

```go
type peerRouter struct{ registry *hub.Registry }

func (pr peerRouter) RoundTrip(req *http.Request) (*http.Response, error) {
    peer := req.Header.Get("x-tunnel-peer")
    req.Header.Del("x-tunnel-peer")
    return pr.registry.RoundTripper(peer).RoundTrip(req)   // down the tunnel
}
```

The peer's gRPC server never listens; the hub reaches it purely by
dialing back through the tunnel the peer opened.

> The demo trusts the peer's self-reported `--id` (sent as an
> `x-peer-id` header on attach) and does no auth on the operator
> endpoint — it's a local demo. In production, authenticate the peer at
> attach (see `authenticated` / `transport-tls`) and protect the
> operator endpoint. For TLS, secure the hops as in `transport-tls` /
> `encrypted`.
