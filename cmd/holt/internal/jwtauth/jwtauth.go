// Package jwtauth issues and verifies the HMAC-SHA256 JWTs peers
// present when attaching to the hub. The token's subject is the peer
// id the hub uses as the tunnel key.
package jwtauth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const issuer = "holt-hub"

// Issue mints a signed JWT for peer, valid for ttl.
func Issue(secret []byte, peer string, ttl time.Duration) (string, error) {
	now := time.Now()

	claims := jwt.RegisteredClaims{
		Issuer:    issuer,
		Subject:   peer,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
	}

	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signed, err := tok.SignedString(secret)
	if err != nil {
		return "", fmt.Errorf("jwtauth: sign: %w", err)
	}

	return signed, nil
}

// Verify checks a token's signature and expiry and returns the peer id
// (subject). The signing method is pinned to HS256.
func Verify(secret []byte, token string) (string, error) {
	parsed, err := jwt.ParseWithClaims(token, &jwt.RegisteredClaims{},
		func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("jwtauth: unexpected signing method %q", t.Header["alg"])
			}

			return secret, nil
		},
		jwt.WithIssuer(issuer),
		jwt.WithValidMethods([]string{"HS256"}),
	)
	if err != nil {
		return "", fmt.Errorf("jwtauth: verify: %w", err)
	}

	claims, ok := parsed.Claims.(*jwt.RegisteredClaims)
	if !ok || claims.Subject == "" {
		return "", errors.New("jwtauth: token has no subject")
	}

	return claims.Subject, nil
}
