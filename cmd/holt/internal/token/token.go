// Package token is the copy-paste enrollment token that carries what a
// peer needs to join: the hub's tunnel URL (its scheme selects the
// transport) and the JWT that authenticates the peer.
//
// Transport encryption is the deployment's job — a TLS edge, ingress, or
// mesh in front of the hub — so the token carries no certificate: an
// https:// tunnel URL dials standard TLS (verified with the system
// roots), an http:// one dials plaintext h2c.
package token

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
)

// JoinToken bundles a peer's enrollment credential.
type JoinToken struct {
	Peer      string `json:"peer"`
	TunnelURL string `json:"tunnel_url"` // e.g. https://holt.example.com or http://127.0.0.1:7000
	JWT       string `json:"jwt"`        // Bearer credential presented on attach
}

// Encode returns a single-line base64 token.
func (t JoinToken) Encode() string {
	raw, _ := json.Marshal(t)

	return base64.StdEncoding.EncodeToString(raw)
}

// Decode parses a token produced by Encode.
func Decode(s string) (JoinToken, error) {
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return JoinToken{}, fmt.Errorf("token: not valid base64: %w", err)
	}

	var t JoinToken
	if unmarshalErr := json.Unmarshal(raw, &t); unmarshalErr != nil {
		return JoinToken{}, fmt.Errorf("token: invalid payload: %w", unmarshalErr)
	}

	if t.TunnelURL == "" || t.JWT == "" {
		return JoinToken{}, fmt.Errorf(
			"token: incomplete (missing tunnel_url or jwt); re-enroll, the token format changed in v0.11")
	}

	if _, _, _, targetErr := t.Target(); targetErr != nil {
		return JoinToken{}, targetErr
	}

	return t, nil
}

// Target resolves the tunnel URL into what a gRPC client needs: the dial
// address (host:port), the TLS server name to verify, and whether to use
// TLS. An https URL dials standard TLS (system roots); an http URL dials
// plaintext h2c. A URL without a port defaults to 443 (https) or 80
// (http).
func (t JoinToken) Target() (string, string, bool, error) {
	u, err := url.Parse(t.TunnelURL)
	if err != nil {
		return "", "", false, fmt.Errorf("token: invalid tunnel_url %q: %w", t.TunnelURL, err)
	}

	var useTLS bool

	switch u.Scheme {
	case "https":
		useTLS = true
	case "http":
		useTLS = false
	default:
		return "", "", false, fmt.Errorf("token: tunnel_url scheme must be http or https, got %q", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return "", "", false, fmt.Errorf("token: tunnel_url has no host: %q", t.TunnelURL)
	}

	port := u.Port()
	if port == "" {
		if useTLS {
			port = "443"
		} else {
			port = "80"
		}
	}

	return net.JoinHostPort(host, port), host, useTLS, nil
}
