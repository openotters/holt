[🌀 holt](../README.md) · [Docs](README.md) · **How it works**

# How it works

*The tunnel, the handshake, and the four moving parts.*

```
  peer (dials out)                         hub (public)
      │                                       │
      │-- WebSocket upgrade (JWT header) ---->│   auth middleware → peer id
      │-- Hello ----------------------------->│
      │<----------------------------- Welcome │   registry.Attach(peer)
      │                                       │
      │  http2.Server.ServeConn(tunnel,       │   http2.Transport over tunnel:
      │    yourHandler)                       │   registry.RoundTripper(peer)
      │                                       │
      │◀======= HTTP request =================│   client.Do(req) → peer's handler
      │======== HTTP response ===============>│
```

A **peer** that cannot accept inbound connections (behind NAT, in a
locked down container, on a field device) dials out to a **hub**, then
serves an ordinary `http.Handler` back through the connection it opened.
The hub gets an `http.RoundTripper` per peer, and a presence signal for
free. No listener, no inbound port, no published container port on the
peer.

Everything rides a single **WebSocket**: each binary message carries
one `TunnelFrame` (raw bytes in `TunnelFrame.data`, at most 32 KiB per
frame); each side runs a standard HTTP/2 endpoint over that byte
stream, server on the peer, client on the hub. A WebSocket is the
carrier because it passes through what gRPC cannot: CDN public
hostnames (Cloudflare included), access proxies, and HTTP/1.1-only
edges. Presence of the tunnel doubles as the peer's liveness signal.

## The parts

The root package `holt` is the front door: `NewServer` assembles the
whole hub in one call (tunnel endpoint, proxy endpoint, lifecycle);
`NewClient` wires a peer that attaches and serves a handler back
through the tunnel. Everything a caller needs to configure the two
constructors lives at the root.

The optional pieces are public under `pkg/`, one package per role:

- **`pkg/registry`**: the operator surface (`srv.Registry()` returns
  one) — tracks the live tunnel per peer and hands out the per-peer
  `http.RoundTripper`; attach/detach events double as the presence
  signal.
- **`pkg/attach`**: the `http.Handler` that accepts a peer's
  WebSocket upgrade and registers the tunnel, mountable on your own
  router behind your auth middleware (the peer's credential arrives
  on the upgrade request).
- **`pkg/revproxy`**: the data plane. Picks the target peer from the
  request (`x-tunnel-peer` header, or `<peer>.<domain>` subdomain)
  and dials it through its tunnel. This is what `holt hub` serves on
  its proxy port.
- **`pkg/dial`**: the client side's persistent attach loop — dials
  the hub's WebSocket endpoint, serves the handler over the tunnel,
  and redials with jittered backoff.
- **`pkg/directory`** (+ `sqldir`, `sqlite`, `postgres`): peer
  presence — in-memory for one hub, SQL-backed to share which peer is
  attached to which hub across a fleet.
- **`pkg/blocklist`** (+ `sqlite`, `postgres`): the peer-id denylist
  consulted at attach time, on the same backend choice as presence —
  a fleet sharing PostgreSQL shares its blocks too.
- **`pkg/jwtauth`**, **`pkg/token`**, **`pkg/peername`**: the
  ready-made JWT identity scheme — issue and verify attach tokens,
  decode the copy-paste join token, validate peer ids.

Only the assembly guts stay private: `internal/tunnel`,
`internal/proxy`, `internal/server`, `internal/client`,
`internal/utils` (what the root facade builds on), and
`internal/wire` (the frame-level `net.Conn` adapter, handshake, and
GoAway vocabulary both halves share).

See [Library](library.md) to embed the two halves, or [CLI](cli.md)
for the batteries-included `holt` binary.

---

[← Install](install.md)  ·  [Docs home](README.md)  ·  [CLI →](cli.md)
