# client-server — two real processes

Unlike the other examples (one process, hub and peer wired together in
memory), this is **two standalone binaries** talking over a real
socket — the shape of an actual deployment.

- **`server/`** — a standalone hub. Accepts tunnels on one port and
  exposes an operator HTTP API on another that reaches peers by
  **reverse-proxying through their tunnels**. Peers authenticate with a
  bearer token that maps to their identity.
- **`client/`** — a standalone peer. Dials the hub, serves an HTTP
  handler over the tunnel, and **listens on nothing itself**. Redials
  automatically if the hub restarts.

## Run it

Terminal 1 — the hub:

```bash
go run ./examples/client-server/server
# hub up  tunnels=127.0.0.1:7000  operator_api=127.0.0.1:7001
```

Terminals 2 and 3 — two peers with different tokens:

```bash
go run ./examples/client-server/client --token tok-alice
go run ./examples/client-server/client --token tok-bob
```

Terminal 4 — reach the listenerless peers through the hub:

```bash
curl localhost:7001/peers
# alice   version=client-server-demo attached=...
# bob     version=client-server-demo attached=...

curl localhost:7001/peers/alice/hello
# hello from tok-alice (pid 18450)

curl localhost:7001/peers/bob/hello
# hello from tok-bob (pid 18451)

curl localhost:7001/peers/alice/time
curl -i localhost:7001/peers/carol/hello   # 502 — carol isn't attached
```

Each peer runs in its own process (note the differing pids) with no
inbound port. The only listeners anywhere are the hub's. `curl`
reaches a peer purely because the peer dialed out and the hub proxies
back down that tunnel.

## What it exercises

- **Auth → identity**: a Connect streaming interceptor validates the
  bearer token and stamps the peer id; the hub keys tunnels by that
  authenticated id (`hub` doc, and the `authenticated` example).
- **Operator surface**: `registry.ListTunnels()` for the roster,
  `registry.RoundTripper(id)` as the transport of a reverse proxy.
- **Reconnection**: kill and restart the hub — the peers redial with
  backoff and reappear, no restart needed.
- **Graceful drain**: Ctrl-C the hub and it `StopAllTunnels`, sending
  each peer a `shutting-down` GoAway so they back off and wait.

## Making it secure

The demo uses plaintext h2c so you can watch it work. For production,
secure the outer gRPC hop with TLS — the peer dials with
`credentials.NewTLS(...)` and the hub serves a TLS listener (see
[`../transport-tls`](../transport-tls)); no code here changes shape.
Add inner TLS ([`../encrypted`](../encrypted)) on top if a proxy
terminates the outer hop.
