// Package holt implements a reverse HTTP tunnel over a WebSocket
// carrying a bidirectional protobuf frame stream: a peer that can
// only dial OUT attaches to a hub, then serves an ordinary
// http.Handler back through the attached stream. The hub gets an
// http.RoundTripper per peer and a presence signal for free. The
// carrier is a WebSocket so the tunnel passes through CDN public
// hostnames, access proxies, and HTTP/1.1-only edges that cannot
// proxy gRPC.
//
// This package is the front door for both halves: NewServer
// assembles a hub from options and serves it; NewClient wires a peer
// that attaches to one and serves a handler back through the tunnel.
// Everything needed to configure the two constructors lives here.
//
// The optional surface lives under pkg/ for applications that want
// the pieces directly: pkg/registry (the operator surface —
// Server.Registry returns one), pkg/attach (the WebSocket attach
// handler, for mounting on your own router), pkg/revproxy (the
// data-plane reverse proxy), pkg/dial (the raw attach loop),
// pkg/directory (peer presence, with SQLite and Postgres flavours),
// and pkg/admin (the admin gRPC service). Only the assembly guts and
// the wire protocol are internal.
package holt
