package blockedip_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jami1024/omnihub/internal/service/blockedip"
)

type stubSource struct {
	entries []blockedip.Policy
	err     error
}

func (s *stubSource) ListAll(_ context.Context) ([]blockedip.Policy, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := make([]blockedip.Policy, len(s.entries))
	copy(out, s.entries)
	return out, nil
}

func TestPolicyBlockedReflectsNullLimits(t *testing.T) {
	hard := &blockedip.Policy{IP: "1.2.3.4", Reason: "spam"}
	if !hard.Blocked() {
		t.Errorf("hard block (all limits zero) should report Blocked()=true")
	}
	soft := &blockedip.Policy{IP: "5.6.7.8", RPMLimit: 60}
	if soft.Blocked() {
		t.Errorf("policy with RPM cap is not a hard block")
	}
	tpmOnly := &blockedip.Policy{IP: "9.9.9.9", TPMLimit: 50_000}
	if tpmOnly.Blocked() {
		t.Errorf("policy with TPM cap is not a hard block")
	}
}

func TestPoolRefreshIndexesPolicies(t *testing.T) {
	src := &stubSource{entries: []blockedip.Policy{
		{IP: "1.2.3.4", Reason: "spam"},
		{IP: "2001:db8::1", RPMLimit: 30},
	}}
	p := blockedip.NewPool(src)
	if err := p.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if p.Size() != 2 {
		t.Errorf("Size: want 2, got %d", p.Size())
	}
	if got := p.Lookup("1.2.3.4"); got == nil || !got.Blocked() {
		t.Errorf("1.2.3.4 should be hard-blocked")
	}
	if got := p.Lookup("2001:db8::1"); got == nil || got.Blocked() || got.RPMLimit != 30 {
		t.Errorf("2001:db8::1 should be soft-limited with RPM=30, got %+v", got)
	}
	if p.Lookup("5.6.7.8") != nil {
		t.Errorf("5.6.7.8 should not have a policy")
	}
	if p.Lookup("") != nil {
		t.Errorf("empty IP must never match")
	}
}

func TestPoolRefreshKeepsPreviousOnError(t *testing.T) {
	src := &stubSource{entries: []blockedip.Policy{{IP: "1.2.3.4", Reason: "abuse"}}}
	p := blockedip.NewPool(src)
	if err := p.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	src.err = errors.New("connection refused")
	if err := p.Refresh(context.Background()); err == nil {
		t.Fatalf("Refresh should have returned the source error")
	}
	if p.Lookup("1.2.3.4") == nil {
		t.Errorf("previous policy must survive a failed refresh")
	}
}

func TestPoolConcurrencyAcquireRelease(t *testing.T) {
	p := blockedip.NewPool(&stubSource{})
	if !p.TryAcquireConcurrency("1.1.1.1", 2) {
		t.Fatalf("first acquire should succeed")
	}
	if !p.TryAcquireConcurrency("1.1.1.1", 2) {
		t.Fatalf("second acquire should succeed")
	}
	if p.TryAcquireConcurrency("1.1.1.1", 2) {
		t.Fatalf("third acquire must be rejected — over the cap")
	}
	p.ReleaseConcurrency("1.1.1.1")
	if !p.TryAcquireConcurrency("1.1.1.1", 2) {
		t.Fatalf("after one release, acquire should succeed again")
	}
	// Zero / negative cap disables the gate.
	if !p.TryAcquireConcurrency("9.9.9.9", 0) {
		t.Fatalf("zero cap should short-circuit allow")
	}
}

func TestTPMCacheConsumesAndRefills(t *testing.T) {
	c := blockedip.NewTPMCache()
	const tpm int64 = 60 // = 1 token/sec refill

	// Empty bucket starts at capacity → Allow.
	if !c.Allow("1.2.3.4", tpm) {
		t.Fatalf("fresh bucket must allow")
	}
	// Drain the bucket.
	c.Charge("1.2.3.4", tpm, 60)
	if c.Allow("1.2.3.4", tpm) {
		t.Fatalf("drained bucket should reject")
	}
	// Sleep ~1.2s and the bucket should have ~1 token back.
	time.Sleep(1200 * time.Millisecond)
	if !c.Allow("1.2.3.4", tpm) {
		t.Fatalf("bucket should have refilled at least one token")
	}
}

func TestTPMCacheZeroLimitSkips(t *testing.T) {
	c := blockedip.NewTPMCache()
	if !c.Allow("1.2.3.4", 0) {
		t.Errorf("tpm=0 must be treated as unlimited")
	}
	c.Charge("1.2.3.4", 0, 1_000_000) // no-op
	if !c.Allow("1.2.3.4", 0) {
		t.Errorf("tpm=0 charge must not affect Allow result")
	}
}
