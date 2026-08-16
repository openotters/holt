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

The web console is a small React app under [`ui/`](../ui), embedded into
the binary with `go:embed`. The embedded build (`cmd/holt/internal/webui/dist`)
is generated, not tracked: `task ui:build` populates it, and goreleaser
rebuilds it before a release. A source checkout that skips the build
compiles fine and the console shows a "not built" message.

## Layout

- root package: the public facade — `NewServer` and `NewClient` (see
  [Library](library.md)).
- `hub/`, `dial/`, `hub/sqldir/`: the library halves underneath.
- `internal/wire/`: the shared `Conn`, handshake, and `GoAway`
  vocabulary.
- `api/v1/`: the protobuf definitions and generated stubs.
- `cmd/holt/`: the operator CLI (see [CLI](cli.md)).
- `charts/holt/`: the Helm chart (see [Kubernetes](kubernetes.md)).
- `examples/`: runnable demos (see [Examples](examples.md)).

---

[← Examples](examples.md)  ·  [Docs home](README.md)
