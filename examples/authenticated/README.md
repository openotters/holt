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
hub → unknown ⇒  attached=false (rejected at handshake)
```

The mechanism:

1. A **Connect streaming interceptor** validates the `Authorization`
   header on the `Attach` call and stamps the resolved peer ID onto
   the request context. (A real hub validates a JWT signature or an
   mTLS certificate here — the demo uses a token→name map.)
2. The hub's **`Identity` func** reads that context value. Because the
   ID comes from the authenticated credential and never from the
   handshake, a peer cannot claim to be someone else.
3. `registry.RoundTripper(peerID)` is therefore keyed by a trusted
   identity — dialing "alice" reaches the process that authenticated
   as alice, nothing else.

This is exactly how [openotters](https://github.com/openotters/openotters)
wires it: the agent's JWT `agent_ref` claim is the peer ID, pinned by
the same interceptor that guards every other daemon RPC.
