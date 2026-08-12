# holt examples

Each example is a single-process, runnable demo — a hub and one or
more peers wired together so you can watch a reverse tunnel work.

| Example | Shows |
|---|---|
| [`echo`](echo) | The minimal round-trip: a peer serves an HTTP handler with no listener; the hub reaches it through the tunnel. |
| [`authenticated`](authenticated) | The identity seam: the hub derives each peer's ID from a bearer token, so the RoundTripper is keyed by an authenticated identity the handshake never carries. |
| [`client-server`](client-server) | **Two standalone binaries** over a real socket — a hub that reverse-proxies your `curl` through a listenerless peer's tunnel. The realistic deployment shape. |
| [`transport-tls`](transport-tls) | Two binaries. **Outer** mutual TLS on the gRPC hop; the hub takes each peer's identity from its client certificate's CN. |
| [`encrypted`](encrypted) | Two binaries. **Inner** mutual TLS *inside* the tunnel over a plaintext transport — end-to-end confidentiality and mutual auth even past a TLS-terminating proxy. |
| [`join-token`](join-token) | Mutual TLS with **no cert files**: the server prints a copy-paste join token (CA + client cert + key) you hand to the client's `--token` flag. |
| [`grpc-tunnel`](grpc-tunnel) | Peers serve a real **gRPC** service (health + reflection) over the tunnel; the hub reverse-proxies so you call it with `grpcurl`, routed per-peer by header. |

To write a peer for the **`holt` CLI** specifically (JWT + tunnel URL
from a join token), copy [`cmd/starter-client`](../cmd/starter-client),
it lives next to the CLI because it's coupled to that token format, not
to the library.

Run any of them with `go run ./examples/<name>`.

**Which TLS do I want?** `transport-tls` (outer) for ordinary transport
security; `encrypted` (inner) when a proxy terminates the outer hop and
you still need end-to-end confidentiality or peer-cert authentication.
They compose — see each README for the trade-offs.
