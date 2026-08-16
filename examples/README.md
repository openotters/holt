# holt examples

Two runnable demos, in learning order. Each is a hub and a peer as
**separate programs** — run the server, run the client, then reach
the listenerless peer through the hub's proxy with `curl`.

| Example | Shows |
|---|---|
| [`echo`](echo) | **Start here.** The minimal pair: `holt.NewServer()` with zero configuration, and a peer that listens on nothing, reached through the tunnel it opened. |
| [`authenticated`](authenticated) | The identity seam: the hub derives each peer's ID from a bearer token, so tunnels are keyed by an authenticated identity the handshake never carries. |

```sh
go run ./examples/echo/server           # terminal 1
go run ./examples/echo/client           # terminal 2
curl -H 'x-tunnel-peer: peer' http://127.0.0.1:7002/whoami
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
