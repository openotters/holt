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

- **`hub`**: server side. `NewRegistry` tracks live tunnels per peer;
  `NewHandler` is the `http.Handler` that accepts attachments, mounted
  behind your auth middleware (the peer's credential arrives on the
  upgrade request).
- **`hub/proxy`**: the data plane. An `http.Handler` that picks the
  target peer from the request (`x-tunnel-peer` header, or
  `<peer>.<domain>` subdomain) and dials it through its tunnel. This is
  what `holt hub` serves on its proxy port.
- **`dial`**: client side. `dial.Run` is a persistent attach loop that
  dials the hub's WebSocket endpoint, serves your `http.Handler` over
  the tunnel, and redials with jittered backoff; extra upgrade headers
  carry whatever auth the hub or the edge in front wants.
- **`hub/sqldir`**: a SQL-backed presence directory (SQLite or
  PostgreSQL) for sharing which peer is attached to which hub across a
  fleet.
- **root package**: the shared `Conn` (stream to `net.Conn` adapter),
  handshake, and `GoAway` vocabulary.

See [Library](library.md) to use these directly, or [CLI](cli.md) for
the batteries-included `holt` binary.

---

[← Install](install.md)  ·  [Docs home](README.md)  ·  [CLI →](cli.md)
