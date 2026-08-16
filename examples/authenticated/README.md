# authenticated — the identity seam

Shows how the hub derives a peer's identity from a credential instead
of trusting the handshake. One peer attaches with a bearer token and
the hub reaches it by the identity its token proved; a peer with no
token is rejected before it ever lands in the registry.

```bash
go run ./examples/authenticated
```

Output:

```
hub → alice   ⇒  "hello from alice"
hub → mallory ⇒  attached=false (rejected at the upgrade)
```

The code is split the way a real deployment is: [`server.go`](server.go)
is the hub half (`WithAuthBearer` and the token check),
[`client.go`](client.go) the peer half (`WithBearerToken`), and
[`main.go`](main.go) only wires the demo together.

The mechanism:

1. **`holt.WithAuthBearer(verify)`** is the whole seam for bearer
   tokens: middleware that validates the `Authorization` header on the
   WebSocket upgrade, and the identity that keys the registry by the
   peer id the token proves. `verify` is one func from token to peer
   id — a real hub validates a JWT signature there, the demo uses a
   comparison.
2. Because the ID comes from the authenticated credential and never
   from the handshake, a peer cannot claim to be someone else. Any
   other scheme is `holt.WithMiddleware` (stamp the context) +
   `holt.WithIdentity` (read it back), which is exactly what
   `WithAuthBearer` does inside — a client-certificate CN, a session
   cookie, any verified source works the same way.
3. `srv.Registry().RoundTripper(peerID)` is therefore keyed by a
   trusted identity — dialing "alice" reaches the process that
   authenticated as alice, nothing else.
4. The peer side is one option: `holt.WithBearerToken(token)` puts
   the credential on the upgrade request.

This is exactly how [openotters](https://github.com/openotters/openotters)
wires it: the agent's JWT `agent_ref` claim is the peer ID, pinned by
the same middleware that guards every other daemon endpoint.

Next: [`../client-server`](../client-server) runs the two halves as
separate programs — the realistic deployment shape.
