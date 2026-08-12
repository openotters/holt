[🌀 holt](../README.md) · [Docs](README.md) · **CLI**

# CLI

*Run a hub, enroll peers, expose services, and manage tunnels with the holt command.*

The operator CLI for a reverse-tunnel hub: run the hub, enroll peers,
and manage live tunnels. Peers authenticate with a **JWT**; the tunnel
transport is encrypted with the hub's **self-signed certificate**, which
the client **pins** (it travels in the join token). Reach a peer's
tunneled service through the hub by naming it in a header.

## Cheat sheet

```sh
holt hub                        # run the hub (tunnel, admin, proxy listeners)
holt hub --ui                   # same, plus the web console on the admin port
holt enroll <peer>              # mint a join token for a peer
holt expose <addr> --token <t>  # expose a local HTTP service through the hub
holt info                       # snapshot of the hub (build, counts, addresses)
holt ls                         # list live tunnels
holt kill <peer>                # disconnect a tunnel (the peer may reconnect)
holt block <peer>               # disconnect AND ban the peer id
holt unblock <peer>             # lift a block
holt renew                      # regenerate the hub cert (invalidates all tokens)
```

Run `holt <cmd> --help` for details.

## State

The hub keeps everything under `~/.holt` (override with `--state`):

- `hub-cert.pem` / `hub-key.pem` / `jwt-secret`: the hub identity, as
  files, reused across restarts so old tokens keep working.
- `holt.db`: a SQLite database holding the **peer blocklist** and the
  **tunnel-presence directory** (via `hub/sqldir`).

## Run a hub

```sh
holt hub
# hub up   tunnel=127.0.0.1:7000  admin=127.0.0.1:7001  proxy=127.0.0.1:7002
```

| Listener | Default | Purpose |
|---|---|---|
| `--tunnel-addr` | `127.0.0.1:7000` | TLS + JWT; peers attach here |
| `--admin-addr`  | `127.0.0.1:7001` | the Admin service (list / stop / block / enroll), serves the console with `--ui` |
| `--proxy-addr`  | `127.0.0.1:7002` | reach a peer's service via the `x-tunnel-peer` header |

The bind address is not always the address peers can dial (behind a
LoadBalancer or NAT). Set `--advertise-addr` to the reachable tunnel
address; the hub stamps that into every token it mints, instead of the
bind address.

## Enroll a peer

`enroll` produces a join token. Two modes:

**Local** (on the hub machine, offline): it reads the cert + JWT secret
from the state folder and signs the token itself. Pass the tunnel
address to advertise:

```sh
holt enroll alice --tunnel-addr 192.168.1.10:7000
```

**Remote** (against a running hub): give it an admin endpoint (see
[remote hubs](#remote-hubs-and-profiles)) and the hub mints the token,
stamping its own `--advertise-addr`, so you do not pass `--tunnel-addr`:

```sh
holt enroll alice --admin-url https://holt.example.com
holt enroll alice --profile prod            # same, via a profile
```

The token bundles the peer's JWT, the hub's address, and the hub's
certificate to pin, so the client encrypts the tunnel **and** verifies
the hub, while the JWT authenticates the client.

Two ready-made peers consume the token:

- `holt expose`: tunnel an **existing local HTTP service**
  (`holt expose localhost:3000 --token …`), ngrok-style.
- [`cmd/starter-client`](../cmd/starter-client): a minimal template that
  serves a built-in demo; copy it to write your own. See
  [Examples](examples.md).

## Reach the peer through the hub

Address a peer's service by its id in the `x-tunnel-peer` header; the
hub proxies the request down that peer's tunnel:

```sh
curl -H 'x-tunnel-peer: alice' http://localhost:7002/
```

Whatever the peer serves over the tunnel (HTTP, or gRPC, see the
`grpc-tunnel` example) is reachable this way.

## Inspect the hub

`holt info` prints a snapshot of a running hub, the CLI view of the
console's status card:

```sh
holt info
# 🌀 holt 0.8.0 (532632e)
#   endpoint   http://127.0.0.1:7001
#   tunnels    3                          live
#   blocked    1                          banned peer ids
#   advertise  192.168.8.193:7000         address stamped into tokens
#   proxy      127.0.0.1:7002             reach peers via the x-tunnel-peer header
#   metrics    127.0.0.1:7003/metrics     prometheus
#   token ttl  24h0m0s                    lifetime of minted tokens
```

It takes the same `--admin-url` / `--profile` / `--header` flags as the
other management commands, so `holt info --profile prod` inspects a
remote hub.

## Manage tunnels

```sh
holt ls                 # list live tunnels
holt kill alice         # disconnect the tunnel; the peer MAY reconnect
holt block alice        # disconnect AND ban the peer id; it CANNOT reconnect
holt unblock alice      # lift the ban
```

A block bans the peer **id** (the JWT subject), not one specific token:
while blocked, every token for that id is refused, even a freshly
enrolled one. Unblocking re-admits the peer, including tokens minted
before the block that have not expired yet, since blocking never
invalidates the tokens themselves.

`ls` / `kill` / `block` / `unblock` call the hub's **Admin** gRPC
service (`openotters.holt.v1.Admin`), reusable from grpcurl or any gRPC
client.

## Remote hubs and profiles

The management commands default to the hub's admin listener on
`127.0.0.1:7001`. To reach a remote hub, point them at its URL and, if
it sits behind an authenticating proxy, add the headers that proxy
expects:

```sh
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
    tunnel_addr: 192.168.8.193:7000   # advertised in tokens enroll mints
    headers:
      # Any headers work. This example is a Cloudflare Access service
      # token; the secret is read from an env var, not stored here.
      CF-Access-Client-Id: 0a1b2c3d.access
      CF-Access-Client-Secret: ${HOLT_PROD_CF_SECRET}
```

```sh
holt ls                          # uses default_profile (prod)
holt ls --profile local          # switch profile
HOLT_PROFILE=local holt kill x   # or via env
holt ls --admin-url http://127.0.0.1:7001   # explicit flag wins over the profile
```

Every value follows one precedence: **flag > env (`HOLT_*`) > profile >
built-in default**, so `--admin-url` beats `HOLT_ADMIN_URL` beats the
profile's `admin_url`, and likewise for `--header`, `--tunnel-addr`, and
`--profile` itself. Header values expand `${ENV}` references, so secrets
stay in the environment and out of the file. The proxy fronting the hub
is your business; holt just sends the headers you give it.

`enroll` reads the same profile: `holt enroll web --profile prod` mints
remotely, and the advertised tunnel address resolves `--tunnel-addr` >
`HOLT_TUNNEL_ADDR` > the profile's `tunnel_addr` > (remote) the hub's own
`--advertise-addr` > (local) `127.0.0.1:7000`.

## Output modes

The CLI is friendly by default: a welcome banner when the hub or
`expose` starts, readable logs, tables and checkmarks. For production
(systemd, containers, log collectors), switch back to the classic
structured JSON logs:

```sh
holt hub --log-format json      # or HOLT_LOG_FORMAT=json
```

`-D` enables debug-level logging in either mode. A first `Ctrl-C` drains
gracefully (peers get a `GoAway`, listeners finish in-flight requests); a
second one forces the process to exit now.

## Renew the certificate

The hub's self-signed certificate is pinned by peers through their join
token. Renewing it generates a fresh one and **invalidates every token
already handed out**, so peers must be re-enrolled.

```sh
holt renew                 # asks for confirmation first
holt renew --yes           # skip the prompt (automation)
```

From the CLI you renew the files, then restart the hub to serve the new
cert. The web console's **Danger zone** does both at once: it renews and
hot-swaps the serving certificate immediately (no restart) and closes
live tunnels, then you re-enroll your peers.

## Security notes

The admin and proxy listeners are **unauthenticated**: keep them on
loopback (the default) or front them with your own auth. Join tokens are
**bearer credentials**; deliver them over a secure channel and keep
`--token-ttl` short. See [Security](security.md) for exposing a hub
safely.

---

[← How it works](architecture.md)  ·  [Docs home](README.md)  ·  [Web console →](console.md)
