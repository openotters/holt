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

The code is split the way a real deployment is: [`server.go`](server.go)
is the hub half, [`client.go`](client.go) the peer half, and
[`main.go`](main.go) only wires the demo together.

Key calls, in order:

1. `holt.NewServer()` + `srv.Run(ctx)` — the whole hub with zero
   configuration: tunnel on `127.0.0.1:7000`, proxy on `:7002`. No
   identity is configured, so the **development identity** applies:
   the peer names itself with the `x-holt-peer` header, nothing
   verifies the claim, and the tunnel must stay on loopback (a wider
   bind refuses to start).
2. `holt.NewClient(url, handler, ...)` + `c.Run(ctx)` — the peer
   attaches over a `ws://` WebSocket and serves its handler back.
3. `srv.Registry().RoundTripper(peer)` wrapped in an `http.Client` —
   the hub dials the peer through the tunnel like any other backend.

While it runs, the proxy reaches the peer from a shell too:

```bash
curl -H 'x-tunnel-peer: peer' http://127.0.0.1:7002/whoami
```

Next: [`../authenticated`](../authenticated) replaces the development
identity with a real one.
