[🌀 holt](../README.md) · **Docs**

# holt documentation

Reverse HTTP tunnels for services that can only dial out. Start with the
[local quickstart](../README.md#quickstart-local), then dig in below.

## Get started

- **[Install](install.md)**: Get the holt binary, image, or Helm chart.
- **[How it works](architecture.md)**: The tunnel, the handshake, and the four moving parts.

## Use holt

- **[CLI](cli.md)**: Run a hub, enroll peers, expose services, and manage tunnels with the holt command.
- **[Web console](console.md)**: The holt hub --ui React console for operating a hub from the browser.

## Operate

- **[Security](security.md)**: Expose the admin and proxy listeners safely, and advertise the address peers can dial.
- **[Kubernetes](kubernetes.md)**: Deploy the hub with the Helm chart.
- **[Observability](observability.md)**: Prometheus metrics and OpenTelemetry instrumentation.

## Build with holt

- **[Library](library.md)**: Embed the hub and dial halves in your own Go program.
- **[Examples](examples.md)**: Runnable demos of every mode.
- **[Development](development.md)**: Build, test, and contribute.
