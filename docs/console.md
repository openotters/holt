[🌀 holt](../README.md) · [Docs](README.md) · **Web console**

# Web console

*The holt hub --ui React console for operating a hub from the browser.*

<p align="center">
  <img src="console-capture.png" alt="holt web console" width="640" />
</p>

`holt hub --ui` serves the console on the admin listener
(`http://127.0.0.1:7201/`). A status light sits next to the console's
name — green while the hub answers, pulsing red when it does not — and
opens the hub's identity card on hover: endpoint, version, route
header, token TTL, and the tunnel/proxy URLs, one click to copy.

Three pages.

## Tunnels

- The live tunnel list, updated over a server stream, with per-peer
  **Kill**, **Block**, and **Call** (the `curl` that reaches the peer
  through the proxy).
- Per-peer **Traffic**: the requests the hub carried, live. Filter by
  path, method, or status class (`5` matches every 5xx), sort any
  column, click a row for the full entry — headers, a bounded slice of
  each body, sizes, timing. Take it away as JSON, or as a command that
  replays it: curl, plain URL, PowerShell, fetch, wget.

  Credential-carrying headers are always `<redacted>`; bodies are
  bounded by `--traffic-body-size` (4 KiB default) and
  `--no-traffic-payloads` keeps the view to metadata. Nothing is
  stored: the hub keeps the last `--traffic-buffer` requests in memory
  (100 default) and forgets them on restart.
- **Add peer** mints a join token; **Recent activity** shows attaches
  and detaches with reasons; **Blocked peers** lists the bans.

## Capture

Capture endpoints on the left, the selected one's live traffic on the
right — the same table as the Tunnels traffic view.

A capture endpoint is a throwaway peer run by the hub itself: it
attaches through the tunnel listener like any peer, accepts any call,
and answers a small JSON receipt. Give its address to a webhook
sender, an OAuth redirect, or a curl, and inspect what arrives —
payloads included — without exposing a real service. Endpoints expire
on their own (an hour by default) and can be deleted early.

## Settings

- **This hub**: routing mode, proxy port, external URL, tunnel URL,
  metrics — the values commands and tokens are built from.
- **Install holt**: the command per method (Homebrew, Go/binary,
  Docker, Kubernetes).
- **Danger zone**: rotate the signing secret — invalidates every
  issued token and closes existing tunnels, same as
  `holt rotate-secret`.

---

The console is built from `cmd/holt/ui/` and embedded in the binary
(`task ui:build`); `--ui-path DIR` serves a local build instead. It
speaks the same Admin service as the CLI, over Connect-JSON.

Set `--external-url https://peers.example.com` when the proxy has a
public address; Call commands then show it.

Exposing the console publicly? See [Security](security.md): it has no
built-in auth and must sit behind an authenticating proxy, with the
host guard configured.

---

[← CLI](cli.md)  ·  [Docs home](README.md)  ·  [Security →](security.md)
