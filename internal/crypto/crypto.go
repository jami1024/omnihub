// Package crypto provides at-rest encryption for sensitive account
// fields (credentials, proxy URLs, custom header values). It uses
// AES-256-GCM with a single master key supplied via environment. The
// design goals are:
//
//   - Zero-downtime rollout: ciphertext carries a "enc:v1:" marker, so
//     legacy plaintext values (written before encryption was enabled)
//     are detected and passed through on read. A boot-time re-encrypt
//     pass migrates them.
//   - Fail-safe disabling: with no key configured the Cipher is a
//     transparent passthrough, preserving the previous plaintext
//     behaviour.
//   - Tamper-evidence: GCM authenticates the ciphertext, so a corrupted
//     or wrong-key value fails loudly rather than returning garbage.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// marker prefixes every ciphertext so reads can distinguish encrypted
// values from legacy plaintext. The version segment allows a future
// scheme change without ambiguity.
const marker = "enc:v1:"

// Cipher encrypts and decrypts short secret strings. The zero value and
// a nil *Cipher are valid passthrough ciphers (encryption disabled).
type Cipher struct {
	aead    cipher.AEAD
	enabled bool
}

// New builds a Cipher from a master key. An empty key returns a disabled
// (passthrough) Cipher and a nil error — encryption stays off, matching
// the previous plaintext behaviour. A non-empty key must decode (base64
// std or hex) to exactly 32 bytes (AES-256).
func New(key string) (*Cipher, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return &Cipher{enabled: false}, nil
	}
	raw, err := decodeKey(key)
	if err != nil {
		return nil, err
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes (AES-256), got %d", len(raw))
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, fmt.Errorf("init AES: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("init GCM: %w", err)
	}
	return &Cipher{aead: aead, enabled: true}, nil
}

// decodeKey accepts a base64-standard or hex-encoded key.
func decodeKey(s string) ([]byte, error) {
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := hex.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	// Surface the base64 attempt's error shape for the common case.
	if _, err := base64.StdEncoding.DecodeString(s); err != nil {
		return nil, errors.New("encryption key is not valid base64 or hex")
	}
	return nil, errors.New("encryption key must decode to 32 bytes")
}

// Enabled reports whether encryption is active.
func (c *Cipher) Enabled() bool { return c != nil && c.enabled }

// IsEncrypted reports whether s carries the ciphertext marker.
func IsEncrypted(s string) bool { return strings.HasPrefix(s, marker) }

// EncryptString returns the marked ciphertext for s. An empty string or
// a disabled cipher returns s unchanged. An already-encrypted value is
// returned as-is (idempotent — never double-encrypts).
func (c *Cipher) EncryptString(s string) (string, error) {
	if !c.Enabled() || s == "" || IsEncrypted(s) {
		return s, nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("nonce: %w", err)
	}
	ct := c.aead.Seal(nil, nonce, []byte(s), nil)
	return marker + base64.RawURLEncoding.EncodeToString(append(nonce, ct...)), nil
}

// DecryptString reverses EncryptString. A value without the marker is
// assumed to be legacy plaintext and returned unchanged (so reads keep
// working during rollout). A marked value with no key configured is an
// error — the operator removed the key that the data needs.
func (c *Cipher) DecryptString(s string) (string, error) {
	if !IsEncrypted(s) {
		return s, nil // legacy plaintext or empty
	}
	if !c.Enabled() {
		return "", errors.New("value is encrypted but no OMNIHUB_ENCRYPTION_KEY is configured")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(s, marker))
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	ns := c.aead.NonceSize()
	if len(raw) < ns {
		return "", errors.New("ciphertext too short")
	}
	nonce, ct := raw[:ns], raw[ns:]
	pt, err := c.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt (wrong key or tampered data): %w", err)
	}
	return string(pt), nil
}

// EncryptMapValues returns a copy of m with every value encrypted (keys
// untouched). Disabled cipher returns m unchanged.
func (c *Cipher) EncryptMapValues(m map[string]string) (map[string]string, error) {
	if !c.Enabled() || len(m) == 0 {
		return m, nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		ev, err := c.EncryptString(v)
		if err != nil {
			return nil, err
		}
		out[k] = ev
	}
	return out, nil
}

// DecryptMapValues decrypts every value of m in place-safe fashion
// (returns a new map). Legacy plaintext values pass through.
func (c *Cipher) DecryptMapValues(m map[string]string) (map[string]string, error) {
	if len(m) == 0 {
		return m, nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		dv, err := c.DecryptString(v)
		if err != nil {
			return nil, err
		}
		out[k] = dv
	}
	return out, nil
}

// HasLegacyPlaintext reports whether any non-empty value lacks the
// ciphertext marker — i.e. the row predates encryption and should be
// re-encrypted. Used by the boot-time migration to skip already-encrypted
// rows.
func HasLegacyPlaintext(values ...string) bool {
	for _, v := range values {
		if v != "" && !IsEncrypted(v) {
			return true
		}
	}
	return false
}
