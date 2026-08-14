// Package token is the copy-paste enrollment token that carries what a
// peer needs to join: the hub's tunnel URL (its scheme selects the
// transport) and the JWT that authenticates the peer.
//
// Transport encryption is the deployment's job — a TLS edge, ingress, or
// mesh in front of the hub — so the token carries no certificate: a
// wss:// tunnel URL dials TLS under the WebSocket (verified with the
// system roots), a ws:// one dials plaintext. https:// and http:// are
// accepted as aliases so tokens minted before the WebSocket carrier
// keep working.
package token

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/openotters/holt/dial"
)

// JoinToken bundles a peer's enrollment credential.
type JoinToken struct {
	Peer      string `json:"peer"`
	TunnelURL string `json:"tunnel_url"` // e.g. wss://holt.example.com or ws://127.0.0.1:7000
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

	if _, wsErr := t.WSURL(); wsErr != nil {
		return JoinToken{}, wsErr
	}

	return t, nil
}

// WSURL resolves the tunnel URL into its WebSocket form: ws and wss
// pass through, http maps to ws and https to wss (pre-WebSocket
// tokens keep working). Any other scheme, or a missing host, is an
// error.
func (t JoinToken) WSURL() (string, error) {
	u, err := dial.NormalizeURL(t.TunnelURL)
	if err != nil {
		return "", fmt.Errorf("token: %w", err)
	}

	return u, nil
}
