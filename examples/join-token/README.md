# join-token — copy-paste credentials, no filesystem

Mutual TLS without sharing any cert files. The server generates a CA
**in memory**, prints a one-line **join token** (base64 of the CA +
this peer's client certificate and key), and serves the tunnel over
mutual TLS. You paste the token into the client's `--token` flag —
nothing touches disk.

This is the alternative to the file-based [`transport-tls`](../transport-tls)
and [`encrypted`](../encrypted) examples: same mutual-TLS security,
distributed by copy-paste instead of a shared directory.

## Run it

```bash
go run ./examples/join-token/server
```

The server prints a ready-to-run line:

```
──────────────── client join token ────────────────
go run ./examples/join-token/client --token eyJjYSI6...<~2.5 KB base64>...
────────────────────────────────────────────────────
```

Copy it into another terminal and run it. The server then reaches the
peer through the tunnel to prove it worked:

```
hub up (mutual TLS, token-issued client cert)       {"addr": "127.0.0.1:7400"}
tunnel attached                                     {"peer": "peer"}
reached peer through tunnel (token-authenticated)   {"peer": "peer", "reply": "hello from a token-joined peer (pid 27872)"}
```

## How

```go
// server: in-memory CA, hand out a client bundle as one token
pki, _ := certs.NewPKI()
hubCert, _ := pki.ServerCert(certs.Hub)      // the hub's own TLS cert
bundle, _  := pki.ClientBundle("peer")       // CA + client cert + key
fmt.Println(bundle.Encode())                 // <- the copy-paste token
// serve mutual TLS: Certificates=[hubCert], RequireAndVerifyClientCert, ClientCAs=pki.Pool()

// client: rebuild the whole TLS config from the token
bundle, _ := certs.DecodeBundle(token)
tlsCfg, _ := bundle.ClientTLS(certs.Hub)      // present cert, verify hub via bundled CA
grpc.NewClient(hubAddr, grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
```

The peer's identity at the hub is still its **client-certificate CN**
(minted into the token), verified against the CA — not anything the
peer asserts. The token is a bearer credential: it contains a private
key, so treat it like one. This demo prints it to stdout because that
is the point; a real system would deliver it over a secure channel and
scope/expire it.

> Example scaffolding only — the in-memory PKI issues long-lived,
> unconstrained certs. Don't reuse it for anything real.
