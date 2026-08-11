# starter-client — a peer you can copy

The smallest useful holt peer: it joins a hub with a token from
`holt enroll`, serves **one** HTTP handler over the tunnel, and
listens on nothing. Copy `main.go` and replace the handler with your
own service — it has no dependency on the holt CLI's internals (the
join token is decoded inline).

```bash
# on the hub machine
holt hub &
holt enroll myservice --hub-addr 127.0.0.1:7000   # prints a token

# anywhere (paste the token)
go run ./cmd/starter-client --token eyJ...

# reach it through the hub
curl -H 'x-tunnel-peer: myservice' http://localhost:7002/
#   hello from myservice — you reached / through the tunnel
```

## The four steps every peer does

`main.go` is organised around them:

1. **Pin the hub cert** from the token → TLS that encrypts the tunnel
   and authenticates the hub.
2. **Dial the hub** with a bearer-JWT interceptor → the hub
   authenticates the peer.
3. **Build your handler** — the one thing you customise.
4. **`dial.Run`** — attach and serve, redialing automatically.

That's the whole client. Everything else (the CLI, TLS files, SQLite,
the admin API) is the hub's concern.
