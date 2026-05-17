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

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/ir"
	"github.com/jami1024/omnihub/internal/service/forward"
	"github.com/jami1024/omnihub/internal/service/guard"
	"github.com/jami1024/omnihub/internal/service/provider"
)

// AnthropicMessagesHandler returns a gin.HandlerFunc for the
// Anthropic-compatible POST /v1/messages endpoint.
//
// The handler is intentionally thin: read the body, decode into IR,
// lift the anthropic-beta header into the IR, and delegate to the
// Forwarder. All transformation lives in the Driver; the Forwarder
// owns transport.
func AnthropicMessagesHandler(
	forwarder *forward.Forwarder,
	driver provider.Driver,
	account *provider.Account,
) gin.HandlerFunc {
	return func(c *gin.Context) {
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

		status, err := forwarder.Forward(c.Request.Context(), c.Writer, &req, driver, account)
		if err != nil {
			slog.Error("forward failed",
				"model", req.Model,
				"stream", req.Stream,
				"upstream_status", status,
				"err", err.Error(),
			)
			// If headers were already written (the streaming case),
			// we cannot send a clean error envelope; the error is
			// logged and the partial response stands.
			return
		}
	}
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
