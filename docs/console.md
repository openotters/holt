[🌀 holt](../README.md) · [Docs](README.md) · **Web console**

# Web console

*The holt hub --ui React console for operating a hub from the browser.*

<p align="center">
  <img src="console.png" alt="holt web console" width="640" />
</p>

`holt hub --ui` serves a small React console on the admin listener
(`http://127.0.0.1:7201/`). A status light sits next to the console's
name — green while the hub answers, pulsing red when it does not —
and opens the hub's identity card on hover: endpoint, version, route
header, token TTL, and the tunnel/proxy URLs, each one click to copy.

The console is three pages.

## Tunnels

The operating view:

- the live tunnel list, updated over a server stream (no polling), with
  per-peer **Kill**, **Block**, and a **Call** button that shows the
  `curl` command to reach that peer through the proxy.
- a per-peer **Traffic** button: the requests the hub carried to that
  peer, live, as a table. Filter it (a path fragment, a method, `5`
  for every 5xx), sort any column, and click a row to open the request
  as a structured entry: a foldable JSON tree carrying the host,
  query, protocol, client, user agent, both sizes and the duration
  (tunnel hop included). Two buttons take it away: **copy** puts the
  entry on the clipboard as JSON, and **curl** puts a command that
  replays the request through the hub (addressed the way this hub
  routes), with a menu next to it for the other forms — plain URL,
  PowerShell, fetch, wget.
  It is per peer on purpose — a fleet's traffic in one list is
  unreadable — and the hub does the filtering, so watching one peer
  never ships you the others'.

  The entry carries the headers and a bounded slice of each body
  (`--traffic-body-size`, 4 KiB by default). Credential-carrying
  header values are always `<redacted>`, a body past the limit is
  marked truncated, and one that is not text is named rather than
  shown. `--no-traffic-payloads` keeps the view to metadata only.

  Nothing is stored: the hub keeps the last `--traffic-buffer`
  requests in memory (100 by default, `0` keeps none) so the table is
  not blank when it opens, forgets them on restart, and closing the
  panel loses the rest. This is the hub's live view; `holt hub` itself
  logs no requests.
- an **Add peer** dialog that mints a join token (shown in full, one
  click to copy).
- a **Recent activity** feed of attaches and detaches, with the reason
  the hub sent (superseded, connection-lost, an operator kill).
- the **Blocked peers** list, with per-peer unblock.

## Capture

The request inspector: capture endpoints on the left, the selected
one's live traffic on the right — the same live table as the Tunnels
page's Traffic view, side by side with the list instead of behind a
modal.

A capture endpoint is a throwaway peer that accepts any call and
answers a small JSON receipt. It runs inside the hub process but
attaches through the tunnel listener like any peer — named like one
too, `sunny-brook-92ae63` — so the proxy routes to it, the roster
lists it, and every request it receives lands in the traffic view,
payloads included. Give its address to a webhook sender, an OAuth
redirect, or a teammate's curl, and inspect what arrives without
exposing a real service.

Endpoints expire on their own (an hour by default) and can be deleted
early; nothing about them survives the hub process.

## Settings

What you touch once:

- a **This hub** card reading the wiring back: routing mode, proxy
  port, external URL, tunnel URL, metrics — the values commands and
  tokens are built from.
- an **Install holt** card: a tile per method (Homebrew, Go/binary,
  Docker, Kubernetes) that opens the command in a modal.
- a **Danger zone** to rotate the hub's signing secret. It regenerates
  the JWT secret, which invalidates every issued token and closes
  existing tunnels, so peers must re-enroll (the same effect as
  `holt rotate-secret`).

---

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
