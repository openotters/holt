[🌀 holt](../README.md) · [Docs](README.md) · **CLI**

# CLI

*Run a hub, enroll peers, expose services, and manage tunnels with the holt command.*

The operator CLI for a reverse-tunnel hub: run the hub, enroll peers,
and manage live tunnels. Peers authenticate with a **JWT** and attach
over a **WebSocket**, so the tunnel passes through CDNs, access
proxies, and plain HTTP ingresses. The listener is plaintext, so
transport encryption is the deployment's job (a TLS edge, ingress, or
mesh in front of the hub); the join token carries the tunnel **URL**
and its scheme picks the transport (`wss` dials TLS verified with the
system roots, `ws` dials plaintext; `https`/`http` are accepted as
aliases). Reach a peer's tunneled service through the hub by naming it
in a header.

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
holt rotate-secret              # rotate the JWT secret (invalidates all tokens)
```

Run `holt <cmd> --help` for details.

## State

The hub keeps everything under `~/.holt` (override with `--state`):

- `jwt-secret`: the hub's JWT signing secret, a file reused across
  restarts so already-issued tokens keep working.
- `holt.db`: a SQLite database holding the **peer blocklist** and the
  **tunnel-presence directory**.

All three move to a shared PostgreSQL with `--directory-dsn` (env
`HOLT_DIRECTORY_DSN`):

```sh
holt hub --directory-dsn postgres://user:pass@db.example.com:5432/holt
```

A fleet of hubs pointed at the same database sees which peer is
attached where, shares one denylist, and **signs with one identity**:
a token minted by any hub verifies at every hub, and rotating on one
rotates for all of them (the others adopt it within 30 seconds,
closing the tunnels the old secret authenticated). Only the owning hub
can proxy to a peer's live tunnel.

A hub that already has a local `jwt-secret` adopts it into the
database the first time it starts with a DSN, so moving identity off
the disk does not invalidate the tokens already in the field. Nothing
seeds over an identity the fleet has agreed on: a second hub joining
with its own file keeps the shared one.

## Run a hub

```sh
holt hub
# hub up   tunnel=127.0.0.1:7200  admin=127.0.0.1:7201  proxy=127.0.0.1:7202
```

| Listener | Default | Purpose |
|---|---|---|
| `--tunnel-addr` | `127.0.0.1:7200` | JWT auth (WebSocket, plaintext); peers attach here |
| `--admin-addr`  | `127.0.0.1:7201` | the Admin service (list / stop / block / enroll), serves the console with `--ui` |
| `--proxy-addr`  | `127.0.0.1:7202` | reach a peer's service via the `x-tunnel-peer` header |

All three listeners are plaintext: put your own TLS in front (a TLS
edge, ingress, LoadBalancer, or mesh) for anything beyond loopback. See
[Security](security.md).

The bind address is not always the URL peers can dial (behind a
LoadBalancer, NAT, or TLS edge). Set `--advertise-addr` to the reachable
tunnel **URL** (e.g. `wss://holt.example.com`); the hub stamps that
into every token it mints, instead of `ws://` + the bind address. The
scheme matters: `wss` tells peers to dial over TLS, `ws` plaintext.

Because the tunnel is a WebSocket, the edge in front needs nothing
special: any proxy that forwards HTTP/1.1 upgrades works, Cloudflare
public hostnames included. A quiet tunnel stays attached through proxy
idle timeouts, the peer pings the socket every 40 seconds. If an
authenticating proxy (e.g. Cloudflare Access with a service-token
policy) fronts the tunnel hostname, pass its headers on attach:

```sh
holt expose localhost:3000 --token <paste> \
  --header 'CF-Access-Client-Id: <id>.access' \
  --header 'CF-Access-Client-Secret: <secret>'
```

## Enroll a peer

`enroll` produces a join token. Two modes:

**Local** (on the hub machine, offline): it reads the JWT secret from the
state folder and signs the token itself. Pass the tunnel URL to
advertise:

```sh
holt enroll alice --tunnel-url wss://holt.example.com
```

**Remote** (against a running hub): give it an admin endpoint (see
[remote hubs](#remote-hubs-and-profiles)) and the hub mints the token,
stamping its own `--advertise-addr`, so you do not pass `--tunnel-url`:

```sh
holt enroll alice --admin-url https://holt.example.com
holt enroll alice --profile prod            # same, via a profile
```

The token **is** the JWT, in JWS compact serialization
(`header.payload.signature`), so any JWT tool reads it:

```sh
holt enroll alice --tunnel-url wss://holt.example.com
# eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJob2x0LWh1YiIsInN1YiI6…
```

Its **subject** is the peer id and its **audience** is the tunnel URL
to dial, which is everything a peer needs. The JWT authenticates the
client; encryption comes from whatever fronts the hub (a TLS edge,
ingress, or mesh), which the `wss` URL tells the peer to dial over,
verified with the system roots.

Tokens minted before v0.20 were a base64 JSON envelope wrapping the
same JWT. `holt expose` still accepts those, so tokens already handed
out keep working until they expire.

### Peer names

A peer id is a **DNS label**: lowercase letters, digits and dashes,
starting and ending with a letter or digit, at most 63 characters
(`web`, `api-gateway-eu-west-1`). The id doubles as a hostname
wherever the proxy [routes by subdomain](#routing-strategies), so a
name that cannot be a label would be a peer some deployments cannot
reach.

Enroll refuses a non-compliant name, and the hub refuses an attach
carrying one (a token minted by an older hub, say) with a `403` naming
the problem. `kill`, `block`, and `unblock` are deliberately not
validated, so an existing peer with a legacy name can still be
managed.

Two ready-made peers consume the token:

- `holt expose`: tunnel an **existing local HTTP service**
  (`holt expose localhost:3000`), ngrok-style.

  The token is optional. Without one, expose enrolls itself against
  the hub it can reach (`--admin-url` / `--profile`, or the local
  state folder when run on the hub machine), so an ad-hoc tunnel is
  one command:

  ```sh
  holt expose localhost:3000                  # enrolls as e.g. cosy-eddy-aec23e
  holt expose localhost:3000 --peer docs      # …under a name you pick
  holt expose localhost:3000 --token <paste>  # …or reuse an identity
  ```

  Generated names are two words and a short random suffix. The suffix
  carries the uniqueness — attaching under a name already in use
  evicts that peer, and exposing must never depend on reading other
  people's tunnels to avoid it. Where the hub routes by subdomain the
  banner prints the peer's URL, reachable immediately.

  Treat a peer hostname as an address, not a secret: put
  authentication in front of anything sensitive you expose.

  A bare `host:port` is treated as `http://`. An `https://` target is
  verified against the system roots, which appliances (routers, NAS,
  IPMI boards) fail because they serve a self-signed certificate — the
  proxy answers `502`. `--insecure` (env `HOLT_EXPOSE_INSECURE`) skips
  verification **for that local hop only**, and says so at startup:

  ```sh
  holt expose https://192.168.1.1 --token <paste> --insecure
  ```

  It never touches the tunnel or the hub, which stay verified. Use it
  for a device on a trusted network whose certificate you cannot fix,
  not for anything sensitive: an attacker on the path to that target
  can read and alter the traffic.
- [`cmd/starter-client`](../cmd/starter-client): a minimal template that
  serves a built-in demo; copy it to write your own. See
  [Examples](examples.md).

## Reach the peer through the hub

Address a peer's service by its id in the `x-tunnel-peer` header; the
hub proxies the request down that peer's tunnel:

```sh
curl -H 'x-tunnel-peer: alice' http://localhost:7202/
```

Whatever the peer serves over the tunnel is reachable this way.

### Routing strategies

The header works everywhere and needs no DNS, but only a client you
control can set it. `--proxy-routing` picks how the proxy resolves the
target peer:

| Strategy | Peer is named by | Needs |
|---|---|---|
| `header` (default) | `x-tunnel-peer: alice` | nothing |
| `subdomain` | `alice.peers.example.com` | `--proxy-domain`, wildcard DNS + cert |
| `both` | either, header wins | same as subdomain |

```sh
holt hub --proxy-routing both --proxy-domain peers.example.com
curl https://alice.peers.example.com/     # no header needed
```

Subdomain routing is what makes a tunneled service reachable by
anything that only takes a **URL**: a browser, a webhook sender, an
OAuth callback, a DoH client. Point a wildcard record at the proxy and
terminate a wildcard certificate at your edge.

Mind the depth: a wildcard certificate matches exactly one label, so
`*.example.com` covers `alice.example.com` but never
`alice.peers.example.com`. Keeping peers at the first level of a zone
(or giving them their own domain) is what makes the free certificate
work — see [Kubernetes](kubernetes.md#pick-the-domain-depth-before-the-certificate).

Two details worth knowing: hostnames are case-insensitive while peer
ids are not, so subdomain routing only reaches **lowercase** peer ids;
and a request whose host is outside the base domain names no peer at
all, so it gets the landing page rather than someone else's tunnel.

A bare visit to the proxy (no `x-tunnel-peer` header) gets a plain swirl
page (`400`), and naming a peer that is not attached gets a `404`, not a
`502`, so a proxy in front of the hub (Cloudflare, etc.) does not turn a
missing header or an offline peer into a scary bad-gateway page. The page
is only the swirl, on purpose: no peer names or hub details leak through
the proxy.

## Inspect the hub

`holt info` prints a snapshot of a running hub, the CLI view of the
console's status card:

```sh
holt info
# 🌀 holt 0.8.0 (532632e)
#   endpoint   http://127.0.0.1:7201
#   tunnels    3                          live
#   blocked    1                          banned peer ids
#   advertise  wss://holt.example.com   URL stamped into tokens
#   proxy      127.0.0.1:7202             reach peers via the x-tunnel-peer header
#   metrics    127.0.0.1:7203/metrics     prometheus
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
`127.0.0.1:7201`. To reach a remote hub, point them at its URL and, if
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
    admin_url: http://127.0.0.1:7201

  prod:
    admin_url: https://holt.example.com
    tunnel_url: wss://holt.example.com    # advertised in tokens enroll mints
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
holt ls --admin-url http://127.0.0.1:7201   # explicit flag wins over the profile
```

Every value follows one precedence: **flag > env (`HOLT_*`) > profile >
built-in default**, so `--admin-url` beats `HOLT_ADMIN_URL` beats the
profile's `admin_url`, and likewise for `--header`, `--tunnel-url`, and
`--profile` itself. Header values expand `${ENV}` references, so secrets
stay in the environment and out of the file. The proxy fronting the hub
is your business; holt just sends the headers you give it.

`enroll` reads the same profile: `holt enroll web --profile prod` mints
remotely, and the advertised tunnel URL resolves `--tunnel-url` >
`HOLT_TUNNEL_URL` > the profile's `tunnel_url` > (remote) the hub's own
`--advertise-addr` > (local) `ws://127.0.0.1:7200`.

One exception, because a profile describes *one hub*: its `tunnel_url`
and its `headers` belong to the hub its `admin_url` names. Pointing
`--admin-url` / `--admin-addr` at a different hub drops both, so a
token carries the advertised URL of the hub that actually minted it,
and a profile's credentials are never sent to a host it does not
describe. `--tunnel-url` and `--header` are explicit and still apply
either way, and a profile with no `admin_url` (the "mint locally,
advertise the public URL" setup) keeps applying its settings.

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

## Rotate the signing secret

Peers authenticate with a JWT signed by the hub's secret. Rotating that
secret generates a fresh one and **invalidates every token already
handed out** (each was signed with the old secret), so peers must be
re-enrolled. This is the way to revoke every outstanding token at once.

```sh
holt rotate-secret         # asks for confirmation first
holt rotate-secret --yes   # skip the prompt (automation)
```

From the CLI you rotate the file, then restart the hub to load it. The
web console's **Danger zone** does both at once: it rotates and
hot-swaps the live secret immediately (no restart) and closes live
tunnels, then you re-enroll your peers.

## Security notes

All three listeners (tunnel, admin, proxy) are plaintext and the
admin and proxy ones are **unauthenticated**: keep them on loopback (the
default) or front them with your own TLS and auth. The tunnel checks the
JWT but does not encrypt by itself, so for remote peers put TLS in front
and advertise a `wss://` URL. Join tokens are **bearer credentials**;
deliver them over a secure channel and keep `--token-ttl` short. See
[Security](security.md) for exposing a hub safely.

---

[← How it works](architecture.md)  ·  [Docs home](README.md)  ·  [Web console →](console.md)
