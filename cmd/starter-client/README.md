# starter-client — a peer you can copy

The smallest useful holt peer: it joins a hub with a token from
`holt enroll`, serves **one** HTTP handler over the tunnel, and
listens on nothing. Copy `main.go` and replace the handler with your
own service — it has no dependency on the holt CLI's internals (the
join token is decoded inline).

```bash
# on the hub machine
holt hub &
holt enroll myservice        # prints a token

# anywhere (paste the token)
go run ./cmd/starter-client --token eyJ...

# reach it through the hub
curl -H 'x-tunnel-peer: myservice' http://localhost:7002/
#   hello from myservice — reached through the tunnel
```

## The three steps every peer does

`main.go` is organised around them:

1. **Decode the token** (a tiny base64+JSON struct): peer name, tunnel
   URL, JWT.
2. **Build your handler**, the one thing you customise.
3. **`dial.Run`**, attach and serve, redialing automatically. The
   token's tunnel URL picks the transport (`wss` is a TLS WebSocket
   verified with the system roots, `ws` is plaintext, `http`/`https`
   are aliases so older tokens keep working), and the JWT goes out as
   the `Authorization` header of the upgrade request.

That's the whole client. Everything else (the CLI, TLS files, SQLite,
the admin API) is the hub's concern.
