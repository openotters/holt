// Package token is the copy-paste enrollment token that carries
// everything a peer needs to join: the hub's tunnel address, the JWT
// that authenticates it, and the hub's self-signed cert to pin (so the
// TLS-encrypted tunnel is verified, not blindly trusted).
package token

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// JoinToken bundles a peer's enrollment credential.
type JoinToken struct {
	Peer    string `json:"peer"`
	HubAddr string `json:"hub_addr"` // host:port of the hub's tunnel listener
	JWT     string `json:"jwt"`      // Bearer credential presented on attach
	CAPEM   []byte `json:"ca_pem"`   // hub's self-signed cert, pinned by the client
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

	if t.HubAddr == "" || t.JWT == "" || len(t.CAPEM) == 0 {
		return JoinToken{}, fmt.Errorf("token: incomplete (missing hub_addr, jwt, or ca_pem)")
	}

	return t, nil
}
