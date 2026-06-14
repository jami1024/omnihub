package proxypool

import (
	"context"
	"testing"
	"time"

	"github.com/jami1024/omnihub/internal/service/provider"
)

type stubSource struct{ proxies []*provider.Proxy }

func (s *stubSource) ListAll(context.Context) ([]*provider.Proxy, error) { return s.proxies, nil }

func ptr[T any](v T) *T { return &v }

// newPoolAt builds a pool refreshed from the given proxies with a fixed
// clock so expiry is deterministic.
func newPoolAt(t *testing.T, now time.Time, proxies ...*provider.Proxy) *Pool {
	t.Helper()
	p := NewPool(&stubSource{proxies: proxies})
	p.now = func() time.Time { return now }
	if err := p.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	return p
}

func acct(proxyID *int64, legacy string) *provider.Account {
	return &provider.Account{ID: 1, ProxyID: proxyID, ProxyURL: legacy}
}

func TestResolveNoBindingUsesLegacy(t *testing.T) {
	p := newPoolAt(t, time.Now())
	if got := p.Resolve(acct(nil, "socks5://legacy:1080")); got != "socks5://legacy:1080" {
		t.Fatalf("nil ProxyID must fall back to inline proxy_url, got %q", got)
	}
	if got := p.Resolve(acct(nil, "")); got != "" {
		t.Fatalf("no binding + no inline = direct, got %q", got)
	}
}

func TestResolveActiveProxy(t *testing.T) {
	now := time.Now()
	pr := &provider.Proxy{ID: 7, Protocol: "http", Host: "h", Port: 8080,
		Username: "u", Password: "pw", Status: "active"}
	p := newPoolAt(t, now, pr)
	got := p.Resolve(acct(ptr(int64(7)), ""))
	if got != "http://u:pw@h:8080" {
		t.Fatalf("active proxy URL: %q", got)
	}
}

func TestResolveDisabledAndVanished(t *testing.T) {
	now := time.Now()
	p := newPoolAt(t, now, &provider.Proxy{ID: 7, Protocol: "http", Host: "h", Port: 1, Status: "disabled"})
	if got := p.Resolve(acct(ptr(int64(7)), "x")); got != "" {
		t.Fatalf("disabled proxy → direct, got %q", got)
	}
	// proxy_id points at a row that's gone from the pool.
	if got := p.Resolve(acct(ptr(int64(99)), "x")); got != "" {
		t.Fatalf("vanished proxy → direct, got %q", got)
	}
}

func TestResolveExpiryFallback(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	past := now.Add(-time.Hour)
	mk := func(id int64, status string, exp *time.Time, mode string, backup *int64) *provider.Proxy {
		return &provider.Proxy{ID: id, Protocol: "http", Host: "h", Port: int(id), Status: status,
			ExpiresAt: exp, FallbackMode: mode, BackupProxyID: backup}
	}

	// none: expired but advisory → keep using it.
	p := newPoolAt(t, now, mk(1, "active", &past, "none", nil))
	if got := p.Resolve(acct(ptr(int64(1)), "")); got != "http://h:1" {
		t.Fatalf("expired none keeps proxy, got %q", got)
	}

	// direct: expired → direct.
	p = newPoolAt(t, now, mk(2, "active", &past, "direct", nil))
	if got := p.Resolve(acct(ptr(int64(2)), "x")); got != "" {
		t.Fatalf("expired direct → direct, got %q", got)
	}

	// proxy: expired → backup chain to first usable.
	// 3(expired,→4) → 4(expired,→5) → 5(active,unexpired) wins.
	future := now.Add(time.Hour)
	p = newPoolAt(t, now,
		mk(3, "active", &past, "proxy", ptr(int64(4))),
		mk(4, "active", &past, "proxy", ptr(int64(5))),
		mk(5, "active", &future, "none", nil),
	)
	if got := p.Resolve(acct(ptr(int64(3)), "")); got != "http://h:5" {
		t.Fatalf("backup chain should resolve to proxy 5, got %q", got)
	}

	// broken chain (backup missing) → direct.
	p = newPoolAt(t, now, mk(6, "active", &past, "proxy", ptr(int64(404))))
	if got := p.Resolve(acct(ptr(int64(6)), "")); got != "" {
		t.Fatalf("broken chain → direct, got %q", got)
	}
}

func TestRecordProbeDebounce(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	p := newPoolAt(t, now, &provider.Proxy{ID: 1, Protocol: "http", Host: "h", Port: 1, Status: "active"})

	// Un-probed proxy is optimistically healthy.
	if !p.healthyLocked(1) {
		t.Fatal("un-probed proxy must be healthy")
	}
	if _, _, _, ok := p.ProxyHealth(1); ok {
		t.Fatal("ProxyHealth must report not-probed before any probe")
	}

	// Two failures stay healthy (de-bounce); the third trips it.
	p.RecordProbe(1, false, 100)
	p.RecordProbe(1, false, 100)
	if !p.healthyLocked(1) {
		t.Fatal("two consecutive fails should not yet mark unhealthy")
	}
	p.RecordProbe(1, false, 100)
	if p.healthyLocked(1) {
		t.Fatal("third consecutive fail must mark unhealthy")
	}
	// One success restores immediately.
	p.RecordProbe(1, true, 42)
	if !p.healthyLocked(1) {
		t.Fatal("a success must restore health")
	}
	healthy, latency, _, ok := p.ProxyHealth(1)
	if !ok || !healthy || latency != 42 {
		t.Fatalf("ProxyHealth after recovery: healthy=%v latency=%d ok=%v", healthy, latency, ok)
	}
}

func TestResolveUnhealthyDegrades(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	future := now.Add(time.Hour)
	// Primary 1 (unexpired) with backup chain to 2 (healthy).
	p := newPoolAt(t, now,
		&provider.Proxy{ID: 1, Protocol: "http", Host: "h", Port: 1, Status: "active",
			ExpiresAt: &future, FallbackMode: "proxy", BackupProxyID: ptr(int64(2))},
		&provider.Proxy{ID: 2, Protocol: "http", Host: "h", Port: 2, Status: "active", ExpiresAt: &future},
	)
	// Healthy primary → use it.
	if got := p.Resolve(acct(ptr(int64(1)), "")); got != "http://h:1" {
		t.Fatalf("healthy primary should be used, got %q", got)
	}
	// Mark primary unhealthy → degrade to backup even though unexpired.
	for i := 0; i < 3; i++ {
		p.RecordProbe(1, false, 0)
	}
	if got := p.Resolve(acct(ptr(int64(1)), "")); got != "http://h:2" {
		t.Fatalf("unhealthy primary must degrade to backup, got %q", got)
	}

	// Unhealthy with fallback_mode none → direct (no pointless reuse).
	p2 := newPoolAt(t, now, &provider.Proxy{ID: 5, Protocol: "http", Host: "h", Port: 5, Status: "active", FallbackMode: "none"})
	for i := 0; i < 3; i++ {
		p2.RecordProbe(5, false, 0)
	}
	if got := p2.Resolve(acct(ptr(int64(5)), "x")); got != "" {
		t.Fatalf("unhealthy none-mode proxy must go direct, got %q", got)
	}
}

func TestResolveCycleSafe(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	past := now.Add(-time.Hour)
	// 1 → 2 → 1, both expired with proxy fallback: must terminate at "".
	p := newPoolAt(t, now,
		&provider.Proxy{ID: 1, Protocol: "http", Host: "h", Port: 1, Status: "active", ExpiresAt: &past, FallbackMode: "proxy", BackupProxyID: ptr(int64(2))},
		&provider.Proxy{ID: 2, Protocol: "http", Host: "h", Port: 2, Status: "active", ExpiresAt: &past, FallbackMode: "proxy", BackupProxyID: ptr(int64(1))},
	)
	if got := p.Resolve(acct(ptr(int64(1)), "")); got != "" {
		t.Fatalf("cyclic backup chain must terminate at direct, got %q", got)
	}
}
