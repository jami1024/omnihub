package upstreamauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// PKCE (RFC 7636) helpers shared by the browser-OAuth login flow of the
// codex and claude plugins. The verifier is held by the admin layer
// between BeginAuth and ExchangeCallback; only the S256 challenge ever
// leaves the gateway (in the authorize URL).

// generateCodeVerifier returns a 43-char base64url (32 random bytes)
// PKCE code verifier.
func generateCodeVerifier() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("pkce: code_verifier: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// codeChallengeS256 returns base64url(sha256(verifier)) — the S256 PKCE
// challenge.
func codeChallengeS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// generateState returns a random opaque state value for CSRF protection.
func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("pkce: state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
