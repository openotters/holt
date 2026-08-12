[🌀 holt](../README.md) · [Docs](README.md) · **How it works**

# How it works

*The tunnel, the handshake, and the four moving parts.*

```
  peer (dials out)                         hub (public)
      │                                       │
      │-- Tunnel.Attach (bidi gRPC) --------->│   auth middleware → peer id
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

Everything rides a single bidirectional gRPC stream. The stream carries
raw bytes (`TunnelFrame.data`, at most 32 KiB per frame); each side runs
a standard HTTP/2 endpoint over it, server on the peer, client on the
hub. Presence of the tunnel doubles as the peer's liveness signal.

## The four parts

- **`hub`**: server side. `NewRegistry` tracks live tunnels per peer;
  `NewHandler` is the `Tunnel.Attach` implementation you mount behind
  your auth middleware.
- **`dial`**: client side. `dial.Run` is a persistent attach loop that
  serves your `http.Handler` over the tunnel and redials with jittered
  backoff. It rides an existing `*grpc.ClientConn`, so it reuses
  whatever auth interceptors you already attached.
- **`hub/sqldir`**: a SQL-backed presence directory (SQLite or
  PostgreSQL) for sharing which peer is attached to which hub across a
  fleet.
- **root package**: the shared `Conn` (stream to `net.Conn` adapter),
  handshake, and `GoAway` vocabulary.

See [Library](library.md) to use these directly, or [CLI](cli.md) for
the batteries-included `holt` binary.

---

[← Install](install.md)  ·  [Docs home](README.md)  ·  [CLI →](cli.md)
