[🌀 holt](../README.md) · [Docs](README.md) · **Security**

# Security

*Expose the admin and proxy listeners safely, and advertise the address peers can dial.*

The **tunnel** listener is always TLS + JWT: peers authenticate with a
JWT and pin the hub's self-signed certificate (it travels in the join
token). Leave that one on; it is the peer-to-hub security boundary.

The **admin** and **proxy** listeners have no built-in auth and bind to
`127.0.0.1` by default. They are meant to stay on loopback or sit behind
an authenticating proxy (a zero trust tunnel, an auth ingress). When you
do expose them:

- a **Host guard** on the admin/console listener rejects requests whose
  `Host` is not loopback or an allow-listed name, defeating
  DNS-rebinding against the plaintext console. Add your public hostname
  with `--allowed-host` (repeatable; `*` disables it). The `/healthz`
  endpoint is always exempt.
- `--max-conns` caps concurrent tunnel connections.
- the [Helm chart](kubernetes.md) keeps both ingresses disabled by
  default and auto-adds the admin ingress host to the guard.

## Advertised address

`--advertise-addr` is the tunnel address stamped into join tokens (what
peers dial). It defaults to `--tunnel-addr`, the **bind** address, which
is wrong behind a LoadBalancer or NAT (a peer cannot dial `0.0.0.0`).
Set it to the reachable address, for example the tunnel Service's
LoadBalancer IP and port. The Helm chart exposes it as
`hub.advertiseAddr`.

## Reaching the admin API remotely

The admin API mints tokens and can kill/block peers, so treat "can reach
admin" as the token-minting boundary. Behind Cloudflare Access (or any
auth proxy), that boundary is your identity provider. The `holt` CLI
sends whatever headers you configure, so it works with service tokens or
any header-based scheme; see [CLI: remote hubs and profiles](cli.md#remote-hubs-and-profiles).

---

[← Web console](console.md)  ·  [Docs home](README.md)  ·  [Kubernetes →](kubernetes.md)
