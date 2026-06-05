package redemption

import (
	"strings"
	"testing"
)

func TestGenerateFormatAndUniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		c := Generate()
		if !strings.HasPrefix(c, "OMNI-") {
			t.Fatalf("code %q missing OMNI- prefix", c)
		}
		if seen[c] {
			t.Fatalf("duplicate code generated: %q", c)
		}
		seen[c] = true
	}
}

func TestCanonicalIgnoresFormatting(t *testing.T) {
	c := Generate()
	// Same code with lowercase, spaces, and stripped dashes must canonicalize
	// identically — so a user can retype it loosely.
	loose := strings.ToLower(strings.ReplaceAll(c, "-", " "))
	if Canonical(c) != Canonical(loose) {
		t.Fatalf("canonical mismatch: %q vs %q", Canonical(c), Canonical(loose))
	}
	if HashOf(c) != HashOf(loose) {
		t.Fatal("hash should be invariant to formatting")
	}
}

func TestHashOfDiffersPerCode(t *testing.T) {
	if HashOf(Generate()) == HashOf(Generate()) {
		t.Fatal("distinct codes hashed equal")
	}
	if len(HashOf("OMNI-ABCD")) != 64 {
		t.Fatal("hash is not sha256 hex (64 chars)")
	}
}

func TestBatchID(t *testing.T) {
	if a, b := BatchID(), BatchID(); a == b {
		t.Fatal("batch ids should differ")
	}
	if len(BatchID()) != 16 {
		t.Fatalf("batch id length = %d, want 16", len(BatchID()))
	}
}
