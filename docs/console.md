[🌀 holt](../README.md) · [Docs](README.md) · **Web console**

# Web console

*The holt hub --ui React console for operating a hub from the browser.*

<p align="center">
  <img src="console.png" alt="holt web console" width="640" />
</p>

`holt hub --ui` serves a small React console on the admin listener
(`http://127.0.0.1:7001/`):

- the live tunnel list, updated over a server stream (no polling), with
  per-peer **Kill**, **Block**, and a **Call** button that shows the
  `curl` command to reach that peer through the proxy.
- an **enroll** form that mints a join token (shown in full, one click
  to copy).
- an **Install holt on a peer** card: a tile per method (Homebrew,
  Go/binary, Docker, Kubernetes) that opens the command in a modal.
- a **Danger zone** to rotate the hub's signing secret. It regenerates
  the JWT secret, which invalidates every issued token and closes
  existing tunnels, so peers must re-enroll (the same effect as
  `holt rotate-secret`).
- a connection status menu with the endpoint, protocol, and a link to
  the Prometheus `/metrics` endpoint when `--metrics` is on.

The console is built from `ui/` and embedded in the binary
(`task ui:build`); pass `--ui-path DIR` to serve a local build instead.
It calls the same Admin service the CLI uses, over Connect-JSON from the
browser.

Set `--external-url https://peers.example.com` when the proxy is
reachable at a public address; the console's Call command then shows
that URL too, and it appears in the startup banner.

Exposing the console publicly? See [Security](security.md): it has no
built-in auth and must sit behind an authenticating proxy, with the
host guard configured.

---

[← CLI](cli.md)  ·  [Docs home](README.md)  ·  [Security →](security.md)
