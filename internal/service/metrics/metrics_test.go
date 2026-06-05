package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRecordIncrementsCounters(t *testing.T) {
	Record(Sample{
		Provider:     "anthropic",
		Model:        "claude-test-1",
		Status:       200,
		Duration:     time.Second,
		TTFB:         500 * time.Millisecond,
		InputTokens:  10,
		OutputTokens: 5,
		CostUSD:      0.0123,
	})

	if got := testutil.ToFloat64(requestsTotal.WithLabelValues("anthropic", "claude-test-1", "200")); got != 1 {
		t.Errorf("requests_total = %v, want 1", got)
	}
	if got := testutil.ToFloat64(tokensTotal.WithLabelValues("anthropic", "claude-test-1", "input")); got != 10 {
		t.Errorf("input tokens = %v, want 10", got)
	}
	if got := testutil.ToFloat64(tokensTotal.WithLabelValues("anthropic", "claude-test-1", "output")); got != 5 {
		t.Errorf("output tokens = %v, want 5", got)
	}
	if got := testutil.ToFloat64(costUSDTotal.WithLabelValues("anthropic", "claude-test-1")); got != 0.0123 {
		t.Errorf("cost_usd = %v, want 0.0123", got)
	}
}

func TestRecordEmptyProviderModelUsesPlaceholder(t *testing.T) {
	Record(Sample{Provider: "", Model: "", Status: 503, Duration: time.Second})
	if got := testutil.ToFloat64(requestsTotal.WithLabelValues("unknown", "unknown", "503")); got != 1 {
		t.Errorf("placeholder label not used: got %v, want 1", got)
	}
}

func TestHandlerTokenGate(t *testing.T) {
	h := Handler("secret")

	// Missing/wrong token → 401.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("no-token status = %d, want 401", w.Code)
	}

	// Correct token → 200 and a Prometheus exposition body.
	w = httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	r.Header.Set("Authorization", "Bearer secret")
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("token status = %d, want 200", w.Code)
	}
}

func TestHandlerOpenWhenNoToken(t *testing.T) {
	w := httptest.NewRecorder()
	Handler("").ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("open metrics status = %d, want 200", w.Code)
	}
}
