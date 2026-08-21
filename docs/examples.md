[🌀 holt](../README.md) · [Docs](README.md) · **Examples**

# Examples

*Two runnable demos, in learning order.*

Each example under [`examples/`](../examples) is a hub and a peer as
separate programs — run the server, run the client, `curl` through
the proxy:

| Example         | What it shows                                              |
|-----------------|------------------------------------------------------------|
| `echo`          | The minimal pair: zero-config hub, listenerless peer       |
| `authenticated` | The identity seam: bearer-token auth on the attach         |

For example:

```sh
go run ./examples/echo/server    # terminal 1
go run ./examples/echo/client    # terminal 2
curl -H 'x-tunnel-peer: peer' http://127.0.0.1:7202/whoami
# I am the peer; the hub reached me through the tunnel
```

Each example directory has its own README with the exact commands.
TLS wiring (a TLS listener on the tunnel, or end-to-end TLS inside
it) is documented in [Library](library.md).

There is also [`cmd/starter-client`](../cmd/starter-client), a minimal
peer meant to be copied as the starting point of your own.

---

[← Library](library.md)  ·  [Docs home](README.md)  ·  [Development →](development.md)
