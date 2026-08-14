# echo — minimal reverse-tunnel demo

Stands up a hub and a peer in one process. The peer serves an HTTP
handler and **never listens**; the hub reaches that handler by dialing
back through the tunnel the peer opened.

```bash
go run ./examples/echo
```

Output:

```
hub → peer GET /whoami  ⇒  200  "I am the peer; the hub reached me through the tunnel"
```

The only inbound listener in the program is the hub's. That is the
whole point: the peer is reachable without a port, a public address, or
a hole in its firewall.

Key calls, in order:

1. `hub.NewRegistry` + `hub.NewHandler` mounted on a plain HTTP server, the hub side (the handler accepts the WebSocket upgrade itself).
2. `dial.Run(ctx, dial.Options{URL, Handler})`, the peer attaches over a `ws://` WebSocket and serves.
3. `registry.RoundTripper(peer)` wrapped in an `http.Client` — the hub dials through.

For a hub that authenticates peers and derives their identity from a
token, see [`../authenticated`](../authenticated).
