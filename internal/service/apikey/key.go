// Package apikey owns the virtual API key domain: the in-memory
// cache, key derivation / hashing helpers, and the NOTIFY listener.
//
// Keys are sensitive but high-entropy. The gateway stores only a
// sha256 hex hash so a database compromise does not yield usable
// credentials. Authentication recomputes sha256(submitted) and looks
// up the row by indexed hash — O(1) and constant-time on the DB side.
package apikey

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Key is the in-memory shape of one virtual API key row. The
// cleartext value is intentionally absent: the pool only ever sees
// the hash and metadata.
type Key struct {
	ID            int64
	Name          string // unique handle
	Hash          string // sha256 hex, 64 chars
	Label         string // displayed in logs ("alice", "ci-bot")
	Enabled       bool
	DailyUSDLimit *float64
	RPMLimit      *int
	AllowedModels []string // nil / empty = all models
	UserID        *int64   // owning portal user; nil for admin/system keys
	PriceRatio    float64  // owner's billing markup; 1.0 (or no owner) = bill at cost
}

// HashOf computes the canonical key hash. Both the CLI's "key add"
// path and the Auth Guard's lookup path call this so the algorithm
// has exactly one definition.
func HashOf(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// Generate returns a new random 32-byte key encoded as 48 base64url
// characters (without padding) — enough entropy to make brute-force
// against the hash infeasible.
func Generate() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("apikey: read random: %w", err)
	}
	return "omni-" + hex.EncodeToString(buf[:]), nil
}
