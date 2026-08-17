[🌀 holt](../README.md) · [Docs](README.md) · **Web console**

# Web console

*The holt hub --ui React console for operating a hub from the browser.*

<p align="center">
  <img src="console.png" alt="holt web console" width="640" />
</p>

`holt hub --ui` serves a small React console on the admin listener
(`http://127.0.0.1:7201/`):

- the live tunnel list, updated over a server stream (no polling), with
  per-peer **Kill**, **Block**, and a **Call** button that shows the
  `curl` command to reach that peer through the proxy.
- a per-peer **Traffic** button: the requests the hub carried to that
  peer, live, as a table. Filter it (a path fragment, a method, `5`
  for every 5xx), sort any column, and click a row to open the request
  as a structured entry: a foldable JSON tree carrying the host,
  query, protocol, client, user agent, both sizes and the duration
  (tunnel hop included). Two buttons take it away: **copy** puts the
  entry on the clipboard as JSON, **curl** puts a command that replays
  the request through the hub (addressed the way this hub routes).
  Bodies are never captured, so the curl carries none.
  It is per peer on purpose — a fleet's traffic in one list is
  unreadable — and the hub does the filtering, so watching one peer
  never ships you the others'.

  Nothing is stored: the hub keeps the last `--traffic-buffer`
  requests in memory (100 by default, `0` keeps none) so the table is
  not blank when it opens, forgets them on restart, and closing the
  panel loses the rest. Metadata only, never a body. This is the
  hub's live view; `holt hub` itself logs no requests.
- an **Add peer** dialog that mints a join token (shown in full, one
  click to copy).
- an **Install holt** card, collapsed by default: a tile per method
  (Homebrew, Go/binary, Docker, Kubernetes) that opens the command in
  a modal.
- a **Danger zone** to rotate the hub's signing secret. It regenerates
  the JWT secret, which invalidates every issued token and closes
  existing tunnels, so peers must re-enroll (the same effect as
  `holt rotate-secret`).
- a connection status menu with the endpoint, protocol, and a link to
  the Prometheus `/metrics` endpoint when `--metrics` is on.

The console is built from `cmd/holt/ui/` and embedded in the binary
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
