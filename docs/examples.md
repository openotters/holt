[🌀 holt](../README.md) · [Docs](README.md) · **Examples**

# Examples

*Runnable demos of every mode.*

Every example under [`examples/`](../examples) is runnable with
`go run`:

| Example         | What it shows                                              |
|-----------------|------------------------------------------------------------|
| `echo`          | Smallest end-to-end demo, hub and peer in one process      |
| `client-server` | Standalone hub and peer as two programs                    |
| `authenticated` | Bearer-token auth on the attach, both sides                |
| `transport-tls` | TLS on the outer WebSocket hop (wss with a private CA)     |
| `encrypted`     | Plaintext outer hop, mutual TLS inside the tunnel          |
| `join-token`    | Token-based enrollment, like the CLI does it               |

For example:

```sh
go run ./examples/echo
# hub → peer GET /whoami  ⇒  200  "I am the peer; the hub reached me through the tunnel"
```

Each example directory has its own README with the exact commands.

There is also [`cmd/starter-client`](../cmd/starter-client), a minimal
peer meant to be copied as the starting point of your own.

---

[← Library](library.md)  ·  [Docs home](README.md)  ·  [Development →](development.md)
