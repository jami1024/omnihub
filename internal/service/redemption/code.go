// Package redemption generates and canonicalizes prepaid gift codes. The
// repository stores only the hash; the cleartext is surfaced once at
// generation, mirroring the API-key model.
package redemption

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"strings"
)

// codeAlphabet is uppercase base32 without padding — unambiguous enough
// for humans to read off a screen and retype.
var codeAlphabet = base32.StdEncoding.WithPadding(base32.NoPadding)

// Generate returns a new random code formatted for display, e.g.
// "OMNI-AB12-CD34-EF56-GH78". 15 random bytes → 24 base32 chars.
func Generate() string {
	b := make([]byte, 15)
	if _, err := rand.Read(b); err != nil {
		panic("redemption: entropy source failed: " + err.Error())
	}
	raw := codeAlphabet.EncodeToString(b) // 24 chars
	var sb strings.Builder
	sb.WriteString("OMNI")
	for i := 0; i < len(raw); i += 4 {
		sb.WriteByte('-')
		sb.WriteString(raw[i : i+4])
	}
	return sb.String()
}

// BatchID returns a short random identifier grouping one generation run.
func BatchID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic("redemption: entropy source failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// Canonical strips formatting (dashes, spaces, the OMNI prefix, case) so a
// code typed back with different spacing still matches. Hashing the
// canonical form makes redemption tolerant of user input quirks.
func Canonical(code string) string {
	up := strings.ToUpper(strings.TrimSpace(code))
	var sb strings.Builder
	for _, r := range up {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		}
	}
	s := sb.String()
	return strings.TrimPrefix(s, "OMNI")
}

// HashOf returns the sha256 hex of the canonical code — what the
// repository stores and looks up.
func HashOf(code string) string {
	sum := sha256.Sum256([]byte(Canonical(code)))
	return hex.EncodeToString(sum[:])
}
