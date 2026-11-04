package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// pkcePair is a PKCE (RFC 7636) code verifier and its S256 challenge.
type pkcePair struct {
	verifier  string
	challenge string
}

// newPKCEPair generates a fresh verifier/challenge pair. The verifier is 32
// random bytes, base64url-encoded with no padding — 43 characters, the
// minimum RFC 7636 and Supabase Auth both require (Supabase: "code
// challenge has to be between 43 and 128 characters", verified against a
// live local instance).
func newPKCEPair() (pkcePair, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return pkcePair{}, fmt.Errorf("generating PKCE verifier: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(raw)

	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	return pkcePair{verifier: verifier, challenge: challenge}, nil
}
