# authenticated — the identity seam

Shows how the hub derives a peer's identity from a credential instead
of trusting the handshake. Two peers attach with different bearer
tokens; the hub reaches each by the identity its token proved, and a
peer with no token is rejected before it ever lands in the registry.

```bash
go run ./examples/authenticated
```

Output:

```
hub → alice   ⇒  "hello from alice"
hub → bob     ⇒  "hello from bob"
hub → unknown ⇒  attached=false (rejected at the upgrade)
```

The mechanism:

1. **`hub.WithAuthBearer(verify)`** is the whole seam for bearer
   tokens: middleware that validates the `Authorization` header on the
   WebSocket upgrade, and the identity that keys the registry by the
   peer id the token proves. `verify` is one func from token to peer
   id — a real hub validates a JWT signature there, the demo uses a
   token→name map.
2. Because the ID comes from the authenticated credential and never
   from the handshake, a peer cannot claim to be someone else. Any
   other scheme is `hub.WithMiddleware` (stamp the context) +
   `hub.WithIdentity` (read it back), which is exactly what
   `WithAuthBearer` does inside — [`../transport-tls`](../transport-tls)
   uses that pair for client-certificate identity.
3. `srv.Registry().RoundTripper(peerID)` is therefore keyed by a
   trusted identity — dialing "alice" reaches the process that
   authenticated as alice, nothing else.

This is exactly how [openotters](https://github.com/openotters/openotters)
wires it: the agent's JWT `agent_ref` claim is the peer ID, pinned by
the same middleware that guards every other daemon endpoint.
