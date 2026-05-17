// Package gateway hosts the HTTP handlers that accept client requests
// and dispatch them through the Forwarder.
//
// MVP shape: one handler per upstream protocol (Anthropic Messages,
// later OpenAI Chat Completions, Gemini, etc.), single account, no
// Guard chain. The Guard pipeline will wrap these handlers later
// without changing their internals.
package gateway

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/ir"
	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/forward"
	"github.com/jami1024/omnihub/internal/service/guard"
	"github.com/jami1024/omnihub/internal/service/provider"
)

// AnthropicMessagesHandler returns a gin.HandlerFunc for the
// Anthropic-compatible POST /v1/messages endpoint.
//
// The handler is intentionally thin: read the body, decode into IR,
// lift the anthropic-beta header into the IR, delegate to the
// Forwarder, and (when buffer != nil) enqueue a complete
// MessageRequest row for persistence. All transformation lives in
// the Driver; the Forwarder owns transport.
//
// buffer may be nil, in which case the gateway runs in log-only mode
// (no DB writes) — this keeps `go run` smoke tests working without a
// Postgres dependency.
func AnthropicMessagesHandler(
	forwarder *forward.Forwarder,
	driver provider.Driver,
	account *provider.Account,
	buffer *repository.WriteBuffer,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		startedAt := time.Now()

		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			errorJSON(c, http.StatusBadRequest, "invalid_request_error", "read body: "+err.Error())
			return
		}

		var req ir.UnifiedRequest
		if err := json.Unmarshal(body, &req); err != nil {
			errorJSON(c, http.StatusBadRequest, "invalid_request_error", "invalid JSON: "+err.Error())
			return
		}

		// Lift anthropic-beta from the HTTP header into the IR so the
		// driver re-emits it as a header to the upstream.
		if beta := c.GetHeader("anthropic-beta"); beta != "" && len(req.AnthropicBeta) == 0 {
			req.AnthropicBeta = splitCSV(beta)
		}

		// Surface routing-relevant fields for the RequestLog guard.
		c.Set(guard.CtxKeyModel, req.Model)
		c.Set(guard.CtxKeyStream, req.Stream)

		result, forwardErr := forwarder.Forward(c.Request.Context(), c.Writer, &req, driver, account)
		if forwardErr != nil {
			slog.Error("forward failed",
				"model", req.Model,
				"stream", req.Stream,
				"upstream_status", result.StatusCode,
				"err", forwardErr.Error(),
			)
			// Headers may already be flushed (streaming case); we
			// cannot reliably send an error envelope. The partial
			// response stands, RequestLog records the failure.
		}

		// Surface usage onto the gin.Context for RequestLog.
		c.Set(guard.CtxKeyUsage, result.Usage)
		if result.TTFB > 0 {
			c.Set(guard.CtxKeyTTFB, result.TTFB)
		}

		// Persistence is opt-in: when no buffer is wired the gateway
		// runs in log-only mode.
		if buffer != nil {
			buffer.Enqueue(buildMessageRequest(c, &req, driver, account, &result, forwardErr, startedAt))
		}
	}
}

// buildMessageRequest assembles a complete row for the message_requests
// table from everything the handler learned.
func buildMessageRequest(
	c *gin.Context,
	req *ir.UnifiedRequest,
	driver provider.Driver,
	account *provider.Account,
	result *forward.Result,
	forwardErr error,
	startedAt time.Time,
) repository.MessageRequest {
	duration := time.Since(startedAt).Milliseconds()

	rec := repository.MessageRequest{
		CreatedAt:                startedAt,
		Method:                   c.Request.Method,
		Path:                     c.Request.URL.Path,
		Model:                    req.Model,
		Stream:                   req.Stream,
		ProviderName:             driver.Name(),
		AccountName:              account.Name,
		InputTokens:              result.Usage.InputTokens,
		OutputTokens:             result.Usage.OutputTokens,
		CacheCreationInputTokens: result.Usage.CacheCreationInputTokens,
		CacheReadInputTokens:     result.Usage.CacheReadInputTokens,
		StatusCode:               intPtr(result.StatusCode),
		DurationMs:               int64Ptr(duration),
	}

	if name := guard.KeyName(c); name != "" {
		rec.KeyName = strPtr(name)
	}
	if result.Usage.ActualModel != "" && result.Usage.ActualModel != req.Model {
		rec.ActualModel = strPtr(result.Usage.ActualModel)
	}
	if result.Usage.UpstreamRequestID != "" {
		rec.UpstreamRequestID = strPtr(result.Usage.UpstreamRequestID)
	}
	if result.TTFB > 0 {
		rec.TtfbMs = int64Ptr(result.TTFB.Milliseconds())
	}
	if forwardErr != nil {
		rec.ErrorMessage = strPtr(forwardErr.Error())
	}
	return rec
}

func errorJSON(c *gin.Context, status int, errType, message string) {
	c.JSON(status, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func strPtr(s string) *string { return &s }
func intPtr(v int) *int       { return &v }
func int64Ptr(v int64) *int64 { return &v }
