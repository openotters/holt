// Package token is the copy-paste enrollment token that carries what a
// peer needs to join: the JWT that authenticates it, and the hub's
// tunnel URL to dial.
//
// The token IS the JWT, in JWS compact serialization
// (header.payload.signature). The tunnel URL rides in the audience
// claim, so there is one format on the wire and any JWT tool can read
// it. Tokens minted before v0.20 were a base64 JSON envelope wrapping
// the same JWT; Decode still accepts those so tokens already handed
// out keep working.
//
// Transport encryption is the deployment's job — a TLS edge, ingress,
// or mesh in front of the hub — so the token carries no certificate: a
// wss:// tunnel URL dials TLS under the WebSocket (verified with the
// system roots), a ws:// one dials plaintext. https:// and http:// are
// accepted as aliases.
package token

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt/v5"

	"github.com/openotters/holt/pkg/dial"
)

// JoinToken is the decoded view of a join token.
type JoinToken struct {
	Peer      string // JWT subject
	TunnelURL string // JWT audience: e.g. wss://holt.example.com
	JWT       string // the compact JWS the peer presents on attach
}

// Decode parses a join token: a compact JWS, or the pre-v0.20 base64
// JSON envelope. The signature is NOT verified — the peer holds no
// key, and the hub is the one that checks it on attach.
func Decode(s string) (JoinToken, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return JoinToken{}, errors.New("token: empty")
	}

	jt, err := decode(s)
	if err != nil {
		return JoinToken{}, err
	}

	if jt.TunnelURL == "" || jt.JWT == "" {
		return JoinToken{}, errors.New("token: incomplete (missing tunnel url or jwt); re-enroll")
	}

	if _, wsErr := jt.WSURL(); wsErr != nil {
		return JoinToken{}, wsErr
	}

	return jt, nil
}

// decode dispatches on the shape: a compact JWS has three
// dot-separated segments, the legacy envelope is one base64 blob.
func decode(s string) (JoinToken, error) {
	if strings.Count(s, ".") == 2 {
		return decodeJWS(s)
	}

	return decodeLegacy(s)
}

// decodeJWS reads the claims of a compact JWS without verifying it.
func decodeJWS(s string) (JoinToken, error) {
	claims := &jwt.RegisteredClaims{}

	if _, _, err := jwt.NewParser().ParseUnverified(s, claims); err != nil {
		return JoinToken{}, fmt.Errorf("token: not a valid JWT: %w", err)
	}

	var audience string
	if len(claims.Audience) > 0 {
		audience = claims.Audience[0]
	}

	return JoinToken{Peer: claims.Subject, TunnelURL: audience, JWT: s}, nil
}

// decodeLegacy reads the pre-v0.20 base64 JSON envelope.
func decodeLegacy(s string) (JoinToken, error) {
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return JoinToken{}, fmt.Errorf("token: not a JWT, and not valid base64: %w", err)
	}

	var envelope struct {
		Peer      string `json:"peer"`
		TunnelURL string `json:"tunnel_url"`
		JWT       string `json:"jwt"`
	}

	if unmarshalErr := json.Unmarshal(raw, &envelope); unmarshalErr != nil {
		return JoinToken{}, fmt.Errorf("token: invalid payload: %w", unmarshalErr)
	}

	return JoinToken{Peer: envelope.Peer, TunnelURL: envelope.TunnelURL, JWT: envelope.JWT}, nil
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
