package admin_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jami1024/omnihub/internal/service/admin"
)

func TestIssuerRoundTrip(t *testing.T) {
	iss := admin.NewIssuer([]byte("test-secret"), time.Hour)
	tok, exp, err := iss.Issue("root", 42)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if strings.Count(tok, ".") != 2 {
		t.Errorf("expected three-segment JWT, got %q", tok)
	}
	if time.Until(exp) < 50*time.Minute {
		t.Errorf("expiry should be ~1h away, got %v", time.Until(exp))
	}
	c, err := iss.Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if c.Sub != "root" || c.UID != 42 {
		t.Errorf("claims = %+v, want sub=root uid=42", c)
	}
}

func TestVerifyRejectsTamperedSignature(t *testing.T) {
	iss := admin.NewIssuer([]byte("secret"), time.Hour)
	tok, _, _ := iss.Issue("root", 1)
	tampered := tok[:len(tok)-1] + "A" // flip last sig byte
	if _, err := iss.Verify(tampered); !errors.Is(err, admin.ErrInvalidToken) {
		t.Errorf("tampered sig: want ErrInvalidToken, got %v", err)
	}
}

func TestVerifyRejectsWrongSecret(t *testing.T) {
	a := admin.NewIssuer([]byte("secret-A"), time.Hour)
	b := admin.NewIssuer([]byte("secret-B"), time.Hour)
	tok, _, _ := a.Issue("root", 1)
	if _, err := b.Verify(tok); !errors.Is(err, admin.ErrInvalidToken) {
		t.Errorf("cross-secret verify: want ErrInvalidToken, got %v", err)
	}
}

func TestVerifyRejectsExpired(t *testing.T) {
	// Negative TTL gets clamped to 24h by NewIssuer, so use a tiny ttl
	// and sleep past it.
	iss := admin.NewIssuer([]byte("secret"), time.Millisecond)
	tok, _, _ := iss.Issue("root", 1)
	time.Sleep(1100 * time.Millisecond) // exp is unix-second granular
	if _, err := iss.Verify(tok); !errors.Is(err, admin.ErrTokenExpired) {
		t.Errorf("expired: want ErrTokenExpired, got %v", err)
	}
}

func TestVerifyRejectsAlgNone(t *testing.T) {
	iss := admin.NewIssuer([]byte("secret"), time.Hour)
	// header = {"alg":"none","typ":"JWT"}, payload {"sub":"root"}, sig empty
	// Hand-roll the bytes without going through the issuer.
	const algNone = "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJzdWIiOiJyb290In0."
	if _, err := iss.Verify(algNone); !errors.Is(err, admin.ErrInvalidToken) {
		t.Errorf("alg=none: want ErrInvalidToken, got %v", err)
	}
}

func TestNewIssuerEmptySecretPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on empty secret")
		}
	}()
	admin.NewIssuer(nil, time.Hour)
}
