[🌀 holt](../README.md) · [Docs](README.md) · **Examples**

# Examples

*Three runnable demos, in learning order.*

Every example under [`examples/`](../examples) is runnable with
`go run`:

| Example         | What it shows                                              |
|-----------------|------------------------------------------------------------|
| `echo`          | Smallest end-to-end demo, hub and peer in one process      |
| `authenticated` | The identity seam: bearer-token auth on the attach         |
| `client-server` | Standalone hub and peer as two programs                    |

For example:

```sh
go run ./examples/echo
# hub → peer GET /whoami  ⇒  200  "I am the peer; the hub reached me through the tunnel"
```

The [examples README](../examples/README.md) has the exact commands;
TLS wiring (a TLS listener on the tunnel, or end-to-end TLS inside
it) is documented in [Library](library.md).

There is also [`cmd/starter-client`](../cmd/starter-client), a minimal
peer meant to be copied as the starting point of your own.

---

[← Library](library.md)  ·  [Docs home](README.md)  ·  [Development →](development.md)
