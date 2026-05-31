package admin_test

import (
	"testing"

	"github.com/jami1024/omnihub/internal/service/admin"
)

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := admin.HashPassword("hunter2")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if len(hash) != 60 {
		t.Errorf("bcrypt hash should be 60 bytes, got %d (%q)", len(hash), hash)
	}
	if err := admin.VerifyPassword(hash, "hunter2"); err != nil {
		t.Errorf("Verify same password: %v", err)
	}
	if err := admin.VerifyPassword(hash, "wrong"); err == nil {
		t.Errorf("Verify wrong password: expected error, got nil")
	}
}
