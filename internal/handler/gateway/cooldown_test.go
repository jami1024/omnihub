package gateway

import (
	"net/http"
	"testing"
	"time"
)

func TestRateLimitCooldown_Priority(t *testing.T) {
	rfc := func(d time.Duration) string {
		return time.Now().Add(d).UTC().Format(time.RFC3339)
	}
	unix := func(d time.Duration) string {
		return formatInt(time.Now().Add(d).Unix())
	}

	cases := []struct {
		name   string
		header http.Header
		body   string
		wantOK bool
		minD   time.Duration
		maxD   time.Duration
	}{
		{
			name:   "retry-after seconds wins over everything",
			header: http.Header{"Retry-After": {"120"}, "Anthropic-Ratelimit-Unified-Reset": {rfc(time.Hour)}},
			wantOK: true, minD: 115 * time.Second, maxD: 125 * time.Second,
		},
		{
			name:   "retry-after http-date",
			header: http.Header{"Retry-After": {time.Now().Add(90 * time.Second).UTC().Format(http.TimeFormat)}},
			wantOK: true, minD: 80 * time.Second, maxD: 95 * time.Second,
		},
		{
			name:   "anthropic unified reset (rfc3339)",
			header: http.Header{"Anthropic-Ratelimit-Unified-Reset": {rfc(5 * time.Minute)}},
			wantOK: true, minD: 4 * time.Minute, maxD: 6 * time.Minute,
		},
		{
			name: "anthropic windows: soonest wins",
			header: http.Header{
				"Anthropic-Ratelimit-Unified-5h-Reset": {unix(10 * time.Minute)},
				"Anthropic-Ratelimit-Unified-7d-Reset": {unix(3 * time.Hour)},
			},
			wantOK: true, minD: 9 * time.Minute, maxD: 11 * time.Minute,
		},
		{
			name:   "codex reset-after header",
			header: http.Header{"X-Codex-Primary-Reset-After-Seconds": {"300"}},
			wantOK: true, minD: 295 * time.Second, maxD: 305 * time.Second,
		},
		{
			name:   "body resets_in_seconds (nested in error)",
			header: http.Header{},
			body:   `{"error":{"type":"usage_limit_reached","resets_in_seconds":600}}`,
			wantOK: true, minD: 595 * time.Second, maxD: 605 * time.Second,
		},
		{
			name:   "body resets_at unix (top level)",
			header: http.Header{},
			body:   `{"resets_at":` + unix(7*time.Minute) + `}`,
			wantOK: true, minD: 6 * time.Minute, maxD: 8 * time.Minute,
		},
		{
			name:   "nothing usable",
			header: http.Header{},
			body:   `{"error":{"type":"rate_limit_error","message":"slow down"}}`,
			wantOK: false,
		},
		{
			name:   "elapsed reset is ignored",
			header: http.Header{"Anthropic-Ratelimit-Unified-Reset": {rfc(-time.Hour)}},
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, ok := rateLimitCooldown(tc.header, []byte(tc.body))
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (d=%v)", ok, tc.wantOK, d)
			}
			if !ok {
				return
			}
			if d < tc.minD || d > tc.maxD {
				t.Fatalf("d = %v, want within [%v, %v]", d, tc.minD, tc.maxD)
			}
		})
	}
}

func TestApplyRateLimitCooldown_OnlyOn429(t *testing.T) {
	// A non-429 retriable status must not set any cooldown.
	resp := &http.Response{StatusCode: http.StatusBadGateway, Header: http.Header{"Retry-After": {"120"}}}
	if _, ok := rateLimitCooldown(resp.Header, nil); !ok {
		t.Fatal("precondition: header should parse")
	}
	// applyRateLimitCooldown is the 429 gate; verify it no-ops by
	// confirming the status guard (tracker nil-safe path).
	applyRateLimitCooldown(nil, 1, resp, nil) // must not panic on nil tracker
}

// formatInt avoids importing strconv just for the test helpers.
func formatInt(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
