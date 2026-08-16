# echo — minimal reverse-tunnel pair

The smallest holt deployment: a hub and a peer as **two programs**.
The peer serves an HTTP handler and **never listens**; the hub
reaches that handler by dialing back through the tunnel the peer
opened.

- [`server/`](server) — the hub half: `holt.NewServer()` with zero
  configuration (tunnel on `127.0.0.1:7200`, proxy on `:7202`).
- [`client/`](client) — the peer half: `holt.NewClient` serving
  `/whoami` back over the tunnel.

## Run it

Terminal 1 — the hub:

```bash
go run ./examples/echo/server
```

Terminal 2 — the peer:

```bash
go run ./examples/echo/client
```

Terminal 3 — reach the listenerless peer through the hub's proxy:

```bash
curl -H 'x-tunnel-peer: peer' http://127.0.0.1:7202/whoami
# I am the peer; the hub reached me through the tunnel
```

The only inbound listeners anywhere are the hub's. That is the whole
point: the peer is reachable without a port, a public address, or a
hole in its firewall — `curl` reaches it purely because the peer
dialed out and the hub proxies back down that tunnel. Kill and
restart the hub: the peer redials with backoff and reappears.

No identity is configured, so the **development identity** applies:
the peer names itself with the `x-holt-peer` header, nothing verifies
the claim, and the tunnel refuses to bind anywhere another machine
could reach.

Next: [`../authenticated`](../authenticated) replaces the development
identity with a real one.
