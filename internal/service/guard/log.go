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
// virtual-key label, response size, and the requested upstream model
// when the handler has set it. Health-check endpoints are skipped to
// keep the signal-to-noise ratio readable.
//
// In a future commit this guard will additionally write a row to the
// usage table (model, account, input/output tokens, cost). For the
// MVP it stays a slog-only sink.
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
		if m := Model(c); m != "" {
			attrs = append(attrs, "model", m)
		}
		if Stream(c) {
			attrs = append(attrs, "stream", true)
		}

		slog.Info("request", attrs...)
	}
}
