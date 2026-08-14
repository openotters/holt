# encrypted — mutual TLS inside the tunnel

Two standalone binaries. The **outer** WebSocket hop is **plaintext**, but
each tunnel runs **mutual TLS inside** it: after the plaintext holt
handshake, the peer becomes the inner TLS server and the hub the inner
TLS client, and each authenticates the other by certificate. The
payload is encrypted end-to-end and both processes are
cryptographically authenticated — even though the transport carries no
TLS, as it would after a TLS-terminating proxy.

- **`server/`** — the hub. Plaintext WebSocket transport, but `WithPeerTLS`
  makes it the inner TLS client: presents the `hub` cert, verifies the
  peer's inner server cert against the shared CA. Generates the demo
  CA + certs on first run.
- **`client/`** — the peer. Plaintext `ws://` dial, but `dial.Options.TLSConfig`
  makes it the inner TLS server: presents the `peer` cert and
  `RequireAndVerifyClientCert`. Serves its handler over the encrypted
  tunnel; listens on nothing.

## Run it

```bash
go run ./examples/encrypted/server     # generates the certs
go run ./examples/encrypted/client     # in another terminal
```

The server reaches the peer through the now-encrypted tunnel and logs:

```
hub up (plaintext transport, inner mutual TLS)  {"addr": "127.0.0.1:7200"}
tunnel attached                                 {"peer": "peer"}
reached peer through inner-TLS tunnel           {"peer": "peer", "reply": "secret from peer (pid 25305), mutually authenticated inside the tunnel"}
```

## Both directions

The tunnel inverts the usual TLS roles — the **peer is the TLS server**,
the **hub is the TLS client** — so mutual auth is:

| Direction | Mechanism |
|---|---|
| hub authenticates the peer | pins the peer's **server** cert (`RootCAs` + `ServerName`) |
| peer authenticates the hub | requires the hub's **client** cert (`ClientAuth: RequireAndVerifyClientCert` + `ClientCAs`) |

```go
// hub (inner TLS client)
hub.NewHandler(reg, id, log, hub.WithPeerTLS(&tls.Config{
    RootCAs:      caPool, ServerName: "peer",
    Certificates: []tls.Certificate{hubCert},   // proves the hub
}))

// peer (inner TLS server)
dial.Run(ctx, dial.Options{URL: "ws://...", Handler: mux, TLSConfig: &tls.Config{
    Certificates: []tls.Certificate{peerCert},
    ClientAuth:   tls.RequireAndVerifyClientCert,
    ClientCAs:    caPool,                        // trusts the hub
}})
```

## Outer vs inner

Reach for **outer** mutual TLS ([`../transport-tls`](../transport-tls))
for ordinary transport security. Use **inner** (this example) when a
proxy terminates the outer hop and you still need confidentiality — and
both identities proven — all the way between the two processes. The two
compose. The negative cases (a rogue peer cert, or a hub with no client
cert) are covered by the module's `TestEncryptedTunnel` /
`TestMutualTLSTunnel`.

> The demo PKI (`../certs`) writes unencrypted keys to a temp dir. It
> is example scaffolding — never reuse it for anything real.
