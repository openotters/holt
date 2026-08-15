package jwtauth

import "sync/atomic"

// Secret is the hub's JWT signing secret, held behind an atomic pointer
// so the console's rotate-secret can swap it live — invalidating every
// issued token — without a process restart. Every read goes through
// Get, so the next verification after a rotate already uses the new
// secret.
type Secret struct {
	v atomic.Pointer[[]byte]
}

// NewSecret holds b as the current signing secret.
func NewSecret(b []byte) *Secret {
	s := &Secret{}
	s.Set(b)

	return s
}

// Get returns the current signing secret, or nil before one is set.
func (s *Secret) Get() []byte {
	if p := s.v.Load(); p != nil {
		return *p
	}

	return nil
}

// Set replaces the signing secret. Tokens signed with the previous one
// stop verifying immediately.
func (s *Secret) Set(b []byte) { s.v.Store(&b) }
