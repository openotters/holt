# authenticated — the identity seam

The same two-program shape as [`echo`](../echo), with a real
identity: the hub derives each peer's ID from a bearer token, so the
tunnel registry is keyed by an authenticated identity the handshake
never carries — a peer cannot claim to be someone else.

- [`server/`](server) — the hub half: `WithAuthBearer(peerForToken)`
  guards the upgrade and keys each tunnel by the peer id the token
  proves; the proxy reaches peers, attach/detach events go to the
  log.
- [`client/`](client) — the peer half: `WithBearerToken` puts the
  credential on the upgrade request.

## Run it

Terminal 1 — the hub:

```bash
go run ./examples/authenticated/server
```

Terminals 2 and 3 — two peers with different tokens:

```bash
go run ./examples/authenticated/client --token tok-alice
go run ./examples/authenticated/client --token tok-bob
```

Terminal 4 — reach the listenerless peers through the hub:

```bash
curl -H 'x-tunnel-peer: alice' localhost:7002/hello
# hello from tok-alice (pid 18450)

curl -H 'x-tunnel-peer: bob' localhost:7002/time

curl -i -H 'x-tunnel-peer: carol' localhost:7002/hello   # 404 — carol isn't
                                    # attached (an absent peer is not a
                                    # failing upstream), and the body
                                    # says nothing about the hub
```

And the rejection path — an unknown token is refused at the upgrade
(HTTP 401) and never lands in the registry:

```bash
go run ./examples/authenticated/client --token tok-mallory
# ... "tunnel detached; redialing" — it never attaches
```

## The mechanism

1. **`holt.WithAuthBearer(verify)`** is the whole seam for bearer
   tokens: middleware that validates the `Authorization` header on
   the WebSocket upgrade, and the identity that keys the registry by
   the peer id the token proves. `verify` is one func from token to
   peer id — a real hub validates a JWT signature there, the demo
   uses a token→name map.
2. Because the ID comes from the authenticated credential and never
   from the handshake, dialing "alice" through
   `srv.Registry().RoundTripper("alice")` (or the proxy's
   `x-tunnel-peer` header) reaches the process that authenticated as
   alice, nothing else. Any other scheme is `holt.WithMiddleware`
   (stamp the context) + `holt.WithIdentity` (read it back) — a
   client-certificate CN, a session cookie, any verified source.
3. **Operator surface**: `srv.Registry().Watch(ctx)` narrates
   attach/detach to the log; `holt.WithErrorHook` logs why a request
   could not be proxied.
4. **Lifecycle**: kill and restart the hub — peers redial with
   backoff and reappear. Ctrl-C the hub and `Run` stops every tunnel
   with a `shutting-down` GoAway, drains, and returns.

This is exactly how [openotters](https://github.com/openotters/openotters)
wires it: the agent's JWT `agent_ref` claim is the peer ID, pinned by
the same middleware that guards every other daemon endpoint.

## Making it secure

The demo uses a plaintext `ws://` WebSocket so you can watch it work.
For production, secure the outer hop with TLS: the peer dials `wss://`
and the hub serves a TLS listener — `holt.NewTunnel("",
holt.WithListener(tlsLis), ...)` — with no change of shape here. For
end-to-end TLS inside the tunnel (past a TLS-terminating proxy), pair
the peer's `WithTunnelTLS` with the hub's `WithPeerTLS`; see
[the library guide](../../docs/library.md).
