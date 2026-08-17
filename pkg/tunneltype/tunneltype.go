// Package tunneltype names what a peer carries over its tunnel.
//
// The hub serves HTTP and HTTPS today: both are an http.Handler on the
// peer, HTTPS meaning the payload is TLS end to end (the peer runs a
// TLS server inside the tunnel and the hub dials it with a matching
// client config), so it stays encrypted even where the outer hop is
// terminated at a proxy.
//
// TCP and TLS name raw byte streams. The hub does not carry them yet;
// they exist here so a peer asking for one is refused by name rather
// than attaching and failing later in a way nobody can read.
package tunneltype

import (
	holtv1 "github.com/openotters/holt/api/v1"
)

// Type is what travels through a tunnel.
type Type string

const (
	// HTTP is an http.Handler on the peer, plaintext inside the tunnel.
	HTTP Type = "http"
	// HTTPS is the same, with the payload encrypted end to end.
	HTTPS Type = "https"
	// TCP is a raw byte stream. Reserved: not carried yet.
	TCP Type = "tcp"
	// TLS is a raw byte stream carrying TLS. Reserved: not carried yet.
	TLS Type = "tls"
)

// Carried reports whether the hub can serve this type today. It is
// what the attach path checks, so an unsupported tunnel is refused at
// the handshake with a reason the operator can read.
func (t Type) Carried() bool { return t == HTTP || t == HTTPS }

// String is the value that reaches a metric label and the Admin API.
// It is always one of the constants above, never peer-supplied text:
// a label a peer could invent is a cardinality bomb.
func (t Type) String() string { return string(t) }

// Proto converts to the wire enum.
func (t Type) Proto() holtv1.TunnelType {
	switch t {
	case HTTPS:
		return holtv1.TunnelType_TUNNEL_TYPE_HTTPS
	case TCP:
		return holtv1.TunnelType_TUNNEL_TYPE_TCP
	case TLS:
		return holtv1.TunnelType_TUNNEL_TYPE_TLS
	case HTTP:
		return holtv1.TunnelType_TUNNEL_TYPE_HTTP
	default:
		return holtv1.TunnelType_TUNNEL_TYPE_HTTP
	}
}

// FromProto reads the wire enum. A peer older than the field sends
// UNSPECIFIED, which is HTTP: that is all any of them could carry.
func FromProto(t holtv1.TunnelType) Type {
	switch t {
	case holtv1.TunnelType_TUNNEL_TYPE_HTTPS:
		return HTTPS
	case holtv1.TunnelType_TUNNEL_TYPE_TCP:
		return TCP
	case holtv1.TunnelType_TUNNEL_TYPE_TLS:
		return TLS
	case holtv1.TunnelType_TUNNEL_TYPE_HTTP, holtv1.TunnelType_TUNNEL_TYPE_UNSPECIFIED:
		return HTTP
	default:
		return HTTP
	}
}
