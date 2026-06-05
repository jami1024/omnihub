// Package metrics exposes Prometheus instrumentation for the gateway's
// request path. Collectors register on the default registry at import
// time; the gateway records one Sample per committed response (see
// Record) and serves the registry at /metrics (see Handler).
//
// Label cardinality is kept bounded on purpose: requests are labelled by
// provider / model / status only — never by virtual key or client IP,
// which are unbounded and would explode the time-series count.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	requestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "omnihub_requests_total",
		Help: "Total committed gateway requests by upstream provider, model and HTTP status.",
	}, []string{"provider", "model", "status"})

	requestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "omnihub_request_duration_seconds",
		Help:    "End-to-end gateway request latency in seconds.",
		Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60, 120},
	}, []string{"provider", "model"})

	ttfbSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "omnihub_ttfb_seconds",
		Help:    "Upstream time-to-first-byte for streaming requests, in seconds.",
		Buckets: []float64{0.1, 0.25, 0.5, 1, 2, 3, 5, 10, 20, 30},
	}, []string{"provider", "model"})

	tokensTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "omnihub_tokens_total",
		Help: "Tokens processed by direction (input/output).",
	}, []string{"provider", "model", "direction"})

	costUSDTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "omnihub_cost_usd_total",
		Help: "Accumulated upstream cost in US dollars.",
	}, []string{"provider", "model"})
)

// Sample is the per-request observation recorded after a response commits
// to the client. Zero values are treated as "not measured" and skipped
// where that distinction matters (TTFB, tokens, cost).
type Sample struct {
	Provider     string
	Model        string
	Status       int
	Duration     time.Duration
	TTFB         time.Duration // 0 when not measured (non-streaming)
	InputTokens  int64
	OutputTokens int64
	CostUSD      float64 // 0 when the model is unpriced
}

// Record folds one committed request into the gateway metrics. Safe for
// concurrent use and cheap enough to call inline on the response path.
func Record(s Sample) {
	provider := label(s.Provider)
	model := label(s.Model)

	requestsTotal.WithLabelValues(provider, model, strconv.Itoa(s.Status)).Inc()
	requestDuration.WithLabelValues(provider, model).Observe(s.Duration.Seconds())
	if s.TTFB > 0 {
		ttfbSeconds.WithLabelValues(provider, model).Observe(s.TTFB.Seconds())
	}
	if s.InputTokens > 0 {
		tokensTotal.WithLabelValues(provider, model, "input").Add(float64(s.InputTokens))
	}
	if s.OutputTokens > 0 {
		tokensTotal.WithLabelValues(provider, model, "output").Add(float64(s.OutputTokens))
	}
	if s.CostUSD > 0 {
		costUSDTotal.WithLabelValues(provider, model).Add(s.CostUSD)
	}
}

// label collapses an empty dimension to a stable placeholder so a missing
// provider / model never produces an empty Prometheus label value.
func label(v string) string {
	if v == "" {
		return "unknown"
	}
	return v
}

// Handler serves the metrics registry; mount it at /metrics. When token
// is non-empty it requires "Authorization: Bearer <token>"; otherwise the
// endpoint is open and should be restricted at the reverse proxy instead.
func Handler(token string) http.Handler {
	h := promhttp.Handler()
	if token == "" {
		return h
	}
	want := "Bearer " + token
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != want {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}
