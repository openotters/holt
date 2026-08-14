# transport-tls — secure the outer WebSocket connection (mutual TLS)

Two standalone binaries. The **outer** WebSocket hop is secured with
**mutual TLS**: peers dial in presenting a client certificate, the hub
verifies it against a shared CA, and the hub takes each peer's identity
from its certificate's Common Name — cryptographic identity, not a
header the peer asserts.

- **`server/`** — the hub. TLS listener presenting the `hub` cert,
  `RequireAndVerifyClientCert` against the demo CA. Derives the peer id
  from the verified client cert. On the first run it generates a demo
  CA + certs into a shared temp dir.
- **`client/`** — a peer. Mints a client cert carrying its `--name`
  (signed by the shared CA), presents it on the `wss://` dial, verifies
  the hub's server cert, and serves an HTTP handler over the tunnel.
  Listens on nothing.

## Run it

```bash
go run ./examples/transport-tls/server            # generates the certs

go run ./examples/transport-tls/client --name alice
go run ./examples/transport-tls/client --name bob
```

The server logs each attach and immediately reaches back through the
tunnel to prove the secure path works:

```
hub up (mutual TLS)                                 {"addr": "127.0.0.1:7100"}
tunnel attached                                     {"peer": "alice"}
reached peer through tunnel (cert-authenticated)    {"peer": "alice", "reply": "hello from alice (pid 25193, mutually authenticated)"}
tunnel attached                                     {"peer": "bob"}
reached peer through tunnel (cert-authenticated)    {"peer": "bob", "reply": "hello from bob (pid 25194, ...)"}
```

The peer id (`alice`, `bob`) comes straight from the client
certificate's CN — the hub never trusts a self-asserted name.

## The wiring

```go
// hub: present the server cert, require + verify the peer's client cert
srv.TLSConfig = &tls.Config{
    Certificates: []tls.Certificate{hubCert},
    ClientAuth:   tls.RequireAndVerifyClientCert,
    ClientCAs:    caPool,
}
// identity middleware lifts the verified CN into the request context:
cn := r.TLS.PeerCertificates[0].Subject.CommonName

// peer: present our client cert, verify the hub's server cert; the
// config rides in through the HTTP client the WebSocket upgrade uses
dial.Options{
    URL: "wss://127.0.0.1:7100",
    HTTPClient: &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
        Certificates: []tls.Certificate{clientCert},
        RootCAs:      caPool,
        ServerName:   "hub",
    }}},
}
```

This needs no holt-specific TLS code — the peer hands `dial.Run` a
plain `*http.Client` carrying its TLS config, and the hub is a normal
TLS `http.Server`. For end-to-end confidentiality past a TLS-terminating
proxy, add inner TLS on top — see [`../encrypted`](../encrypted), which
mirrors this pair but does the mutual TLS *inside* the tunnel.

> The demo PKI (`../certs`) writes unencrypted keys to a temp dir. It
> is example scaffolding — never reuse it for anything real.
