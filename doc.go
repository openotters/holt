// Package holt implements a reverse HTTP tunnel over a WebSocket
// carrying a bidirectional protobuf frame stream: a peer that can
// only dial OUT attaches to a hub, then serves an ordinary
// http.Handler back through the attached stream. The hub gets an
// http.RoundTripper per peer and a presence signal for free. The
// carrier is a WebSocket so the tunnel passes through CDN public
// hostnames, access proxies, and HTTP/1.1-only edges that cannot
// proxy gRPC.
//
// This package is the module's entire public API, the front door for
// both halves: NewServer assembles a hub from options and serves it;
// NewClient wires a peer that attaches to one and serves a handler
// back through the tunnel. The operator surface stays reachable
// through Server.Registry (roster, stop, watch, per-peer
// RoundTripper). Everything underneath lives in internal/ packages —
// registry, attach handler, presence directory, reverse proxy, the
// raw attach loop — and is deliberately not importable.
package holt
