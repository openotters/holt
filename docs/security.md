[🌀 holt](../README.md) · [Docs](README.md) · **Security**

# Security

*Expose the hub's listeners safely, and advertise the URL peers can dial.*

All three listeners (tunnel, admin, proxy) are plaintext **h2c**: holt
does not manage a certificate, so transport encryption is the
deployment's job (a TLS edge, ingress, LoadBalancer, or mesh in front of
the hub).

The **tunnel** listener authenticates every peer with a **JWT**, but it
does not encrypt by itself. For remote peers, put TLS in front and
advertise an `https://` URL: the peer then dials over TLS (verified with
the system roots) so the JWT travels encrypted to the edge, which
forwards to the hub. On loopback or a trusted link, `http://` (plaintext
h2c) is fine. Keep `--token-ttl` short: a JWT is a bearer credential.

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

## Advertised URL

`--advertise-addr` is the tunnel **URL** stamped into join tokens (what
peers dial). It defaults to `http://` + `--tunnel-addr`, the **bind**
address, which is wrong behind a LoadBalancer, NAT, or TLS edge (a peer
cannot dial `0.0.0.0`, and a plaintext URL skips your edge's TLS). Set
it to the reachable URL, for example `https://holt.example.com` for a
hub behind a TLS ingress, or `http://<lb-ip>:7000` for a plain
LoadBalancer on a trusted network. Its scheme selects the peer
transport. The Helm chart exposes it as `hub.advertiseAddr`.

## Reaching the admin API remotely

The admin API mints tokens and can kill/block peers, so treat "can reach
admin" as the token-minting boundary. Behind Cloudflare Access (or any
auth proxy), that boundary is your identity provider. The `holt` CLI
sends whatever headers you configure, so it works with service tokens or
any header-based scheme; see [CLI: remote hubs and profiles](cli.md#remote-hubs-and-profiles).

---

[← Web console](console.md)  ·  [Docs home](README.md)  ·  [Kubernetes →](kubernetes.md)
