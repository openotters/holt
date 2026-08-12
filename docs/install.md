[🌀 holt](../README.md) · [Docs](README.md) · **Install**

# Install

*Get the holt binary, image, or Helm chart.*

## Homebrew (macOS and Linux)

```sh
brew install openotters/tap/holt
```

## Binary

Grab a prebuilt binary for your OS and architecture (darwin/linux,
amd64/arm64) from the [releases page](https://github.com/openotters/holt/releases),
or install it with Go:

```sh
go install github.com/openotters/holt/cmd/holt@latest
```

## Docker

Multi-arch images (amd64/arm64) are published on ghcr:

```sh
docker run --rm ghcr.io/openotters/holt:latest --version

# run a hub (bind to 0.0.0.0 so the ports are reachable from outside
# the container). --tmpfs keeps state in memory for a quick try.
docker run --rm -p 7000:7000 -p 7001:7001 -p 7002:7002 --tmpfs /data \
  ghcr.io/openotters/holt:latest hub --state /data \
  --tunnel-addr 0.0.0.0:7000 --admin-addr 0.0.0.0:7001 --proxy-addr 0.0.0.0:7002
```

The image runs as a non-root user, so state on `--tmpfs` is lost on
restart (every join token with it). For durable state give it a `/data`
writable by uid `65532` (a host directory `chown`ed to it, or run with
`--user 0`); on Kubernetes the [Helm chart](kubernetes.md) handles this
with `fsGroup`.

## Kubernetes (Helm)

The chart ships as an OCI artifact next to the images:

```sh
helm install holt oci://ghcr.io/openotters/charts/holt
```

It runs the hub with persistent state, a LoadBalancer for the tunnel
listener, and the admin console and proxy kept private (ingresses off
by default). See [Kubernetes](kubernetes.md) for the full chart guide.

## As a library

```sh
go get github.com/openotters/holt
```

See [Library](library.md) for the hub and dial APIs.

---

[Docs home](README.md)  ·  [How it works →](architecture.md)
