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
// The pieces underneath the facade stay public for applications that
// want to mount or drive them directly — package hub (registry,
// attach handler, presence directory) and hub/proxy on the server
// side, package dial (the raw attach loop) on the client side.
package holt
