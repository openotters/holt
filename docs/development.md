[🌀 holt](../README.md) · [Docs](README.md) · **Development**

# Development

*Build, test, and contribute.*

Everything goes through [Task](https://taskfile.dev):

```sh
task                # list all tasks
task build          # build ./bin/holt
task dev            # run a hub with the web console from source
task test:race      # tests with the race detector
task lint           # golangci-lint (same config as CI)
task gen            # regenerate protobuf stubs (Go + TypeScript)
task ui:build       # rebuild the embedded web console
task chart:lint     # lint and render the Helm chart
task check          # full local gate, same as CI
```

The web console is a small React app under
[`cmd/holt/ui/`](../cmd/holt/ui), embedded into
the binary with `go:embed`. The embedded build (`cmd/holt/internal/webui/dist`)
is generated, not tracked: `task ui:build` populates it, and goreleaser
rebuilds it before a release. A source checkout that skips the build
compiles fine and the console shows a "not built" message.

## Releasing

A `v*` tag publishes: goreleaser writes the GitHub release, pushes the
images and the Helm chart to ghcr, and updates the homebrew tap.

The release workflow runs the lint, test, chart, and UI checks against
the tagged commit first, and publishes nothing unless all four pass. A
tag can be pushed at any commit, including one that never sat on a
green main, so the gate proves the checks pass *there* rather than
trusting they passed somewhere else.

Locally, run the same checks with `task check` before tagging. Use
`GOWORK=off` for lint and test parity: the workspace's parent
`go.work` changes which packages the linter loads, so a run without it
can be greener than CI.

## Layout

- root package: the front door — `NewServer`, `NewClient`, and their
  options (see [Library](library.md)).
- `pkg/`: the optional public pieces, one package per role —
  `registry`, `attach`, `revproxy`, `dial`, `directory` (+ `sqldir`,
  `sqlite`, `postgres`), `blocklist`, `jwtauth`, `token`, and
  `peername` (see [How it works](architecture.md)).
- `internal/`: the assembly guts — `tunnel`, `proxy`, `server`,
  `client`, `utils`, and the `wire` protocol.
- `api/v1/`: the protobuf definitions and generated stubs.
- `cmd/holt/`: the operator CLI (see [CLI](cli.md)).
- `charts/holt/`: the Helm chart (see [Kubernetes](kubernetes.md)).
- `examples/`: runnable demos (see [Examples](examples.md)).

---

[← Examples](examples.md)  ·  [Docs home](README.md)
