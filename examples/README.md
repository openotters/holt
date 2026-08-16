# holt examples

Three runnable demos, in learning order. Each is a hub and one or
more peers wired together so you can watch a reverse tunnel work.

| Example | Shows |
|---|---|
| [`echo`](echo) | **Start here.** The minimal round-trip in one process: `holt.NewServer()` with zero configuration, a peer that listens on nothing, and the hub reaching its handler through the tunnel. |
| [`authenticated`](authenticated) | The identity seam: the hub derives the peer's ID from a bearer token, so the RoundTripper is keyed by an authenticated identity the handshake never carries. |
| [`client-server`](client-server) | **Two standalone binaries** over a real socket — a hub that reverse-proxies your `curl` through a listenerless peer's tunnel. The realistic deployment shape. |

Run the single-process ones directly:

```sh
go run ./examples/echo
go run ./examples/authenticated
```

For `client-server`, start the hub, then a peer, then curl through the
proxy:

```sh
go run ./examples/client-server/server
go run ./examples/client-server/client --token tok-alice   # another terminal
curl -H 'x-tunnel-peer: alice' localhost:7002/hello
```

The tunnel's carrier is a **WebSocket**, so a peer names the hub by
URL and the scheme picks the transport: `ws://` is plaintext, `wss://`
is TLS on the WebSocket hop (`http`/`https` are accepted as aliases).
TLS wiring — a `tls.Listener` on the tunnel via `WithListener`, or
end-to-end TLS inside the tunnel via `WithTunnelTLS`/`WithPeerTLS` —
is documented in [the library guide](../docs/library.md).

To write a peer for the **`holt` CLI** specifically (JWT + tunnel URL
from a join token), copy [`cmd/starter-client`](../cmd/starter-client);
it lives next to the CLI because it's coupled to that token format, not
to the library.
