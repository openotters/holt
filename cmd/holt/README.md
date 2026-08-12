# holt CLI

The operator CLI for a reverse-tunnel hub: run the hub, enroll peers,
and manage live tunnels. Peers authenticate with a **JWT**; the
tunnel transport is encrypted with the hub's **self-signed
certificate**, which the client **pins** (it travels in the join
token). Reach a peer's tunneled service through the hub by naming it in
a header.

```bash
go build -o holt ./cmd/holt
```

## State

The hub keeps everything under `~/.holt` (override with `--state`):

- `hub-cert.pem` / `hub-key.pem` / `jwt-secret` — the hub identity, as
  files, reused across restarts so old tokens keep working.
- `holt.db` — a SQLite database holding the **peer blocklist**
  and the **tunnel-presence directory** (via `hub/sqldir`).

## Run a hub

```bash
holt hub
# hub up   tunnel=127.0.0.1:7000  admin=127.0.0.1:7001  proxy=127.0.0.1:7002
```

| Listener | Default | Purpose |
|---|---|---|
| `--tunnel-addr` | `127.0.0.1:7000` | TLS + JWT; peers attach here |
| `--admin-addr`  | `127.0.0.1:7001` | the **Admin gRPC** service (list / stop / block); serves the web console with `--ui` |
| `--proxy-addr`  | `127.0.0.1:7002` | reach a peer's service via the `x-tunnel-peer` header |

### Web console (optional)

`holt hub --ui` serves a React console on the admin listener
(`http://127.0.0.1:7001/`) — list tunnels, kill/block/unblock a peer,
and enroll a new one (with the run command to copy). It's built from
`ui/` and embedded in the binary (`task ui:build`); pass `--ui-path DIR`
to serve a local build instead. The console calls the same Admin gRPC
service the CLI uses, over Connect-JSON from the browser.

## Enroll a peer

`enroll` mints a token offline, signing it with the hub's stored
identity (run it on the hub machine):

```bash
holt enroll alice --hub-addr 127.0.0.1:7000
#   Join token for "alice" — run your peer with it, e.g.:
#     starter-client --token eyJ...
```

The token bundles the peer's JWT, the hub's address, and the hub's
certificate to pin — so the client encrypts the tunnel **and** verifies
the hub, while the JWT authenticates the client.

Two ready-made peers consume the token:

- `holt expose` — tunnel an **existing local HTTP service**
  (`holt expose localhost:3000 --token …`), ngrok-style.
- [`cmd/starter-client`](../starter-client) — a minimal template that
  serves a built-in demo; copy it to write your own.

## Reach the peer through the hub

Address a peer's service by its id in the `x-tunnel-peer` header; the
hub proxies the request down that peer's tunnel:

```bash
curl -H 'x-tunnel-peer: alice' http://localhost:7002/
```

Whatever the peer serves over the tunnel (HTTP, gRPC — see the
`grpc-tunnel` example) is reachable this way.

## Manage tunnels

```bash
holt ls                 # list live tunnels (Admin gRPC)
holt kill alice         # disconnect the tunnel — the peer MAY reconnect
holt block alice        # disconnect AND ban the peer id — the peer CANNOT reconnect
holt unblock alice      # lift the ban
```

A block bans the peer **id** (the JWT subject), not one specific
token: while blocked, every token for that id is refused, even a
freshly enrolled one. Unblocking re-admits the peer — including tokens
minted before the block that have not expired yet, since blocking
never invalidates the tokens themselves.

## Remote hubs and profiles

The management commands (`ls` / `kill` / `block` / `unblock`) default to
the hub's admin listener on `127.0.0.1:7001`. To reach a remote hub,
point them at its URL and, if it sits behind an authenticating proxy,
add the headers that proxy expects:

```bash
holt ls --admin-url https://holt.example.com \
  --header 'CF-Access-Client-Id: <id>' \
  --header 'CF-Access-Client-Secret: <secret>'
```

Repeating that gets old, so connections can live in profiles at
`~/.holt/config.yaml` (override with `--config` / `HOLT_CONFIG`):

```yaml
default_profile: prod

profiles:
  local:
    admin_url: http://127.0.0.1:7001

  prod:
    admin_url: https://holt.example.com
    headers:
      # Any headers work. This example is a Cloudflare Access service
      # token; the secret is read from an env var, not stored here.
      CF-Access-Client-Id: 0a1b2c3d.access
      CF-Access-Client-Secret: ${HOLT_PROD_CF_SECRET}
```

```bash
holt ls                          # uses default_profile (prod)
holt ls --profile local          # switch profile
HOLT_PROFILE=local holt kill x   # or via env
holt ls --admin-url http://127.0.0.1:7001   # explicit flag wins over the profile
```

Header values expand `${ENV}` references, so secrets stay in the
environment and out of the file. Precedence is flag > env
(`HOLT_ADMIN_URL`, and `${…}` in headers) > profile > built-in default.
The proxy fronting the hub is your business; holt just sends the
headers you give it.

## Output modes

The CLI is friendly by default: a welcome banner when the hub or
`expose` starts, readable logs, tables and checkmarks. For production
(systemd, containers, log collectors), switch back to the classic
structured JSON logs:

```bash
holt hub --log-format json      # or HOLT_LOG_FORMAT=json
```

`-D` enables debug-level logging in either mode.

## Renew the certificate

The hub's self-signed certificate is pinned by peers through their
join token. Renewing it generates a fresh one and **invalidates every
token already handed out** — peers must be re-enrolled.

```bash
holt renew                 # asks for confirmation first
holt renew --yes           # skip the prompt (automation)
```

From the CLI you renew the files, then restart the hub to serve the new
cert. The web console's **Danger zone** does both at once: it renews and
hot-swaps the serving certificate immediately (no restart), then you
re-enroll your peers.

- **kill** sends a terminal GoAway; the running peer stops redialing,
  but the same token still works if it re-runs.
- **block** additionally denies the peer's JWT subject at the door. The
  block is persisted in SQLite, so it survives a hub restart, until you
  `unblock`.

`ls` / `kill` / `block` / `unblock` call the hub's **Admin** gRPC
service (`openotters.holt.v1.Admin`), reusable from grpcurl or any
gRPC client.

## Security notes

Demo defaults are permissive: the admin and proxy listeners are
**unauthenticated** — keep them on loopback (the default) or front them
with your own auth. Join tokens are **bearer credentials**; deliver them
over a secure channel and keep `--token-ttl` short.
