# holt console (web UI)

A small React console for the holt hub — list tunnels, kill /
block / unblock a peer, and enroll a new one. It follows the
[openotters](../../openotters/app) app's design (Tailwind v4 tokens,
shadcn `new-york` primitives, the status-pill language) and talks to
the hub's **Admin** gRPC service via Connect (`@connectrpc/connect-query`),
plus `POST /api/enroll` for the add button.

Stack: Vite + React 19 + TypeScript + Tailwind v4 + connect-query.

## Build & embed

The console is embedded in the `holt` binary and served by
`holt hub --ui`. From the module root:

```bash
task ui:install     # once
task ui:build       # gen TS clients + vite build + copy into the embed dir
task build          # go build picks up the embedded dist
```

`task ui:build` runs `buf generate ..` (into `src/gen`), `npm run build`,
and rsyncs `dist/` into `cmd/holt/internal/webui/dist/` (embedded via
`go:embed`). Commit that embed dir so the binary builds without npm, the
same way openotters commits its UI build.

## Develop

```bash
holt hub --ui &          # a hub to talk to
task ui:dev                 # Vite dev server on :5173, proxying to the hub
```

`vite.config.ts` proxies `/openotters.holt.v1.Admin` and `/api` to
`http://127.0.0.1:7001` (override with `HOLT_ADMIN`).
