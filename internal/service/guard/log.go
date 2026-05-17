package guard

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// RequestLog returns a gin.HandlerFunc that emits a structured log line
// after each request completes.
//
// The log is intentionally compact: method, path, status, duration,
// virtual-key label, response size, and — when the handler has set
// them — the upstream model, stream flag, token usage, and TTFB. The
// fields mirror the columns persisted in message_requests, so the
// log lines are useful even when no database is configured.
//
// Health-check endpoints are skipped to keep the signal-to-noise ratio
// readable.
func RequestLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		path := c.Request.URL.Path
		if path == "/healthz" || path == "/readyz" {
			return
		}

		attrs := []any{
			"method", c.Request.Method,
			"path", path,
			"status", c.Writer.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
			"key_name", KeyName(c),
			"size", c.Writer.Size(),
		}
		if ip := ClientIP(c); ip != "" {
			attrs = append(attrs, "client_ip", ip)
		}
		if ua := UserAgent(c); ua != "" {
			attrs = append(attrs, "user_agent", ua)
		}
		if m := Model(c); m != "" {
			attrs = append(attrs, "model", m)
		}
		if Stream(c) {
			attrs = append(attrs, "stream", true)
		}
		if ttfb := TTFB(c); ttfb > 0 {
			attrs = append(attrs, "ttfb_ms", ttfb.Milliseconds())
		}

		u := Usage(c)
		if u.InputTokens > 0 || u.OutputTokens > 0 {
			attrs = append(attrs,
				"input_tokens", u.InputTokens,
				"output_tokens", u.OutputTokens,
			)
			if u.CacheCreationInputTokens > 0 {
				attrs = append(attrs, "cache_creation_tokens", u.CacheCreationInputTokens)
			}
			if u.CacheReadInputTokens > 0 {
				attrs = append(attrs, "cache_read_tokens", u.CacheReadInputTokens)
			}
		}
		if u.ActualModel != "" && u.ActualModel != Model(c) {
			attrs = append(attrs, "actual_model", u.ActualModel)
		}
		if u.UpstreamRequestID != "" {
			attrs = append(attrs, "upstream_request_id", u.UpstreamRequestID)
		}
		if cost, ok := CostUSD(c); ok {
			attrs = append(attrs, "cost_usd", cost)
		}

		slog.Info("request", attrs...)
	}
}
