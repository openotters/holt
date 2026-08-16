# client-server — two real processes

Unlike the other examples (one process, hub and peer wired together in
memory), this is **two standalone binaries** talking over a real
socket — the shape of an actual deployment.

- **`server/`** — a standalone hub, assembled with `holt.NewServer`:
  an auth-guarded tunnel port peers attach to, and a proxy port that
  reaches them through their tunnels. Peers authenticate with a
  bearer token that maps to their identity.
- **`client/`** — a standalone peer, `holt.NewClient`. Dials the hub,
  serves an HTTP handler over the tunnel, and **listens on nothing
  itself**. Redials automatically if the hub restarts.

## Run it

Terminal 1 — the hub:

```bash
go run ./examples/client-server/server
# holt tunnel up  addr=127.0.0.1:7000
# holt proxy up   addr=127.0.0.1:7002
```

Terminals 2 and 3 — two peers with different tokens:

```bash
go run ./examples/client-server/client --token tok-alice
go run ./examples/client-server/client --token tok-bob
```

Terminal 4 — reach the listenerless peers through the hub:

```bash
curl -H 'x-tunnel-peer: alice' localhost:7002/hello
# hello from tok-alice (pid 18450)

curl -H 'x-tunnel-peer: bob' localhost:7002/hello
# hello from tok-bob (pid 18451)

curl -H 'x-tunnel-peer: alice' localhost:7002/time
curl -i -H 'x-tunnel-peer: carol' localhost:7002/hello   # 404 — carol isn't
                                    # attached (an absent peer is not a
                                    # failing upstream), and the body
                                    # says nothing about the hub
```

Each peer runs in its own process (note the differing pids) with no
inbound port. The only listeners anywhere are the hub's. `curl`
reaches a peer purely because the peer dialed out and the hub proxies
back down that tunnel.

## The whole hub, one call

```go
srv := holt.NewServer(
    holt.WithLogger(logger),
    holt.WithTunnel(holt.NewTunnel(tunnelAddr,   // where peers attach
        holt.WithAuthBearer(peerForToken),       // token → peer id
    )),
    holt.WithProxy(holt.NewProxy(proxyAddr)),    // reach peers: x-tunnel-peer header
)

return srv.Run(ctx) // binds, serves, blocks; Ctrl-C drains
```

## What it exercises

- **Auth → identity**: `WithAuthBearer` guards the attach endpoint
  with a Bearer check and keys the tunnel by the peer id the token
  proves — never by anything the peer asserts.
- **Operator surface**: `srv.Registry().Watch(ctx)` narrates
  attach/detach to the log; the same registry answers roster and
  presence questions.
- **Proxy opinions**: an absent peer answers 404 (it is not a failing
  upstream), a failing tunnel 502, and neither body leaks the peer
  name or any hub detail; `holt.WithErrorHook` logs why a request
  could not be proxied.
- **Reconnection**: kill and restart the hub — the peers redial with
  backoff and reappear, no restart needed.
- **Graceful drain**: Ctrl-C the hub and `Run` stops every tunnel with
  a `shutting-down` GoAway, drains the listeners, and returns.

## Making it secure

The demo uses a plaintext `ws://` WebSocket so you can watch it work.
For production, secure the outer hop with TLS: the peer dials `wss://`
and the hub serves a TLS listener — `holt.NewTunnel("",
holt.WithListener(tlsLis), ...)` — with no change of shape here. For
end-to-end TLS inside the tunnel (past a TLS-terminating proxy), pair
the peer's `WithTunnelTLS` with the hub's `WithPeerTLS`; see
[the library guide](../../docs/library.md).
