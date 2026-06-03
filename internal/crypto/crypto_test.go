package crypto

import (
	"encoding/base64"
	"strings"
	"testing"
)

// 32-byte test key (base64).
func testKey() string {
	return base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
}

func mustCipher(t *testing.T, key string) *Cipher {
	t.Helper()
	c, err := New(key)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestRoundTrip(t *testing.T) {
	c := mustCipher(t, testKey())
	for _, s := range []string{"sk-secret-123", "socks5://user:pass@host:1080", "x"} {
		enc, err := c.EncryptString(s)
		if err != nil {
			t.Fatalf("encrypt: %v", err)
		}
		if !IsEncrypted(enc) {
			t.Errorf("expected marker on %q", enc)
		}
		if enc == s {
			t.Errorf("ciphertext equals plaintext for %q", s)
		}
		dec, err := c.DecryptString(enc)
		if err != nil {
			t.Fatalf("decrypt: %v", err)
		}
		if dec != s {
			t.Errorf("round-trip: got %q want %q", dec, s)
		}
	}
}

func TestEmptyAndIdempotent(t *testing.T) {
	c := mustCipher(t, testKey())
	if e, _ := c.EncryptString(""); e != "" {
		t.Errorf("empty must stay empty, got %q", e)
	}
	enc, _ := c.EncryptString("hello")
	again, _ := c.EncryptString(enc) // already encrypted → unchanged
	if again != enc {
		t.Errorf("double-encrypt changed value: %q != %q", again, enc)
	}
}

func TestDisabledIsPassthrough(t *testing.T) {
	c := mustCipher(t, "") // no key
	if c.Enabled() {
		t.Fatal("empty key must be disabled")
	}
	enc, _ := c.EncryptString("plain")
	if enc != "plain" {
		t.Errorf("disabled encrypt must passthrough, got %q", enc)
	}
	dec, _ := c.DecryptString("plain")
	if dec != "plain" {
		t.Errorf("disabled decrypt must passthrough, got %q", dec)
	}
}

func TestLegacyPlaintextPassesThrough(t *testing.T) {
	c := mustCipher(t, testKey())
	// A value with no marker is legacy plaintext; decrypt returns it as-is.
	dec, err := c.DecryptString("sk-legacy-plaintext")
	if err != nil {
		t.Fatalf("legacy decrypt: %v", err)
	}
	if dec != "sk-legacy-plaintext" {
		t.Errorf("legacy passthrough failed: %q", dec)
	}
}

func TestTamperDetected(t *testing.T) {
	c := mustCipher(t, testKey())
	enc, _ := c.EncryptString("secret")
	// Flip a character in the base64 body.
	body := strings.TrimPrefix(enc, marker)
	tampered := marker + "A" + body[1:]
	if _, err := c.DecryptString(tampered); err == nil {
		t.Error("expected tamper/decrypt error, got nil")
	}
}

func TestWrongKeyFails(t *testing.T) {
	c1 := mustCipher(t, testKey())
	enc, _ := c1.EncryptString("secret")
	c2 := mustCipher(t, base64.StdEncoding.EncodeToString([]byte("FEDCBA9876543210FEDCBA9876543210")))
	if _, err := c2.DecryptString(enc); err == nil {
		t.Error("decrypt with wrong key should fail")
	}
}

func TestEncryptedNoKeyErrors(t *testing.T) {
	c1 := mustCipher(t, testKey())
	enc, _ := c1.EncryptString("secret")
	disabled := mustCipher(t, "")
	if _, err := disabled.DecryptString(enc); err == nil {
		t.Error("decrypting a marked value with no key must error")
	}
}

func TestMapValues(t *testing.T) {
	c := mustCipher(t, testKey())
	in := map[string]string{"api_key": "sk-1", "aws_region": "us-east-1"}
	enc, err := c.EncryptMapValues(in)
	if err != nil {
		t.Fatal(err)
	}
	if !IsEncrypted(enc["api_key"]) || !IsEncrypted(enc["aws_region"]) {
		t.Error("map values should be encrypted")
	}
	if _, ok := enc["api_key"]; !ok {
		t.Error("keys must be preserved")
	}
	dec, err := c.DecryptMapValues(enc)
	if err != nil {
		t.Fatal(err)
	}
	if dec["api_key"] != "sk-1" || dec["aws_region"] != "us-east-1" {
		t.Errorf("map round-trip failed: %+v", dec)
	}
}

func TestHasLegacyPlaintext(t *testing.T) {
	c := mustCipher(t, testKey())
	enc, _ := c.EncryptString("secret")
	if HasLegacyPlaintext(enc, "") {
		t.Error("all-encrypted/empty must report no legacy")
	}
	if !HasLegacyPlaintext(enc, "plain-value") {
		t.Error("a plaintext value must report legacy")
	}
}

func TestBadKey(t *testing.T) {
	if _, err := New("not-base64-or-hex!!"); err == nil {
		t.Error("garbage key should error")
	}
	if _, err := New(base64.StdEncoding.EncodeToString([]byte("too-short"))); err == nil {
		t.Error("short key should error")
	}
}
