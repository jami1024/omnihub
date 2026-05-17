// Package gateway hosts the HTTP handlers that accept client requests
// and dispatch them through the Forwarder.
//
// MVP shape: one handler per upstream protocol (Anthropic Messages,
// later OpenAI Chat Completions, Gemini, etc.). The handler asks the
// Resolver for the (driver, account) pair to use; the Forwarder owns
// transport, and persistence is optional.
package gateway

import (
	"encoding/json"
	"errors"
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
	"github.com/jami1024/omnihub/internal/service/pricing"
	"github.com/jami1024/omnihub/internal/service/provider"
	"github.com/jami1024/omnihub/internal/service/resolver"
)

// anthropicCompatibleProviders is the allow-list for the Anthropic
// Messages endpoint. Both the direct API driver and Claude Platform
// on AWS accept the same wire format.
var anthropicCompatibleProviders = []string{"anthropic", "claude-platform"}

// AnthropicMessagesHandler returns a gin.HandlerFunc for the
// Anthropic-compatible POST /v1/messages endpoint.
//
// The handler is intentionally thin: read the body, decode into IR,
// lift the anthropic-beta header into the IR, ask the Resolver for an
// (account, driver) pair, delegate to the Forwarder, and (when buffer
// != nil) enqueue a complete MessageRequest row for persistence.
//
// buffer may be nil, in which case the gateway runs in log-only mode
// (no DB writes) — this keeps `go run` smoke tests working without a
// Postgres dependency.
func AnthropicMessagesHandler(
	forwarder *forward.Forwarder,
	res resolver.Resolver,
	buffer *repository.WriteBuffer,
	prices pricing.Table,
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

		// Pick the upstream for this request. ErrNoUpstream surfaces
		// when the pool is empty (e.g. all accounts disabled), which
		// is a server-side issue: 503 to the client.
		account, driver, err := res.ResolveForProviders(anthropicCompatibleProviders)
		if err != nil {
			if errors.Is(err, resolver.ErrNoUpstream) {
				errorJSON(c, http.StatusServiceUnavailable, "no_upstream_available",
					"no upstream account is available for this request")
				return
			}
			slog.Error("resolver failed", "err", err.Error())
			errorJSON(c, http.StatusInternalServerError, "internal_error", "resolver failed")
			return
		}

		result, forwardErr := forwarder.Forward(c.Request.Context(), c.Writer, &req, driver, account)
		if forwardErr != nil {
			slog.Error("forward failed",
				"model", req.Model,
				"stream", req.Stream,
				"upstream_status", result.StatusCode,
				"account", account.Name,
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

		// Resolve the cost using the most specific model we know about
		// (upstream-reported actual_model takes priority over the
		// client's requested alias). The account-level multiplier
		// scales every bucket so resellers can mark up or discount.
		var (
			costUSD       *float64
			costBreakdown *pricing.Breakdown
		)
		modelForPricing := result.Usage.ActualModel
		if modelForPricing == "" {
			modelForPricing = req.Model
		}
		if prices != nil && modelForPricing != "" {
			if base, ok := prices.Calculate(modelForPricing, result.Usage); ok {
				final := base.ApplyMultiplier(account.CostMultiplier)
				costBreakdown = &final
				costUSD = &final.Total
			} else {
				slog.Warn("no pricing entry for model",
					"requested_model", req.Model,
					"actual_model", result.Usage.ActualModel,
				)
			}
		}
		if costUSD != nil {
			c.Set(guard.CtxKeyCostUSD, *costUSD)
		}

		// Persistence is opt-in: when no buffer is wired the gateway
		// runs in log-only mode.
		if buffer != nil {
			rec := buildMessageRequest(c, &req, driver, account, &result, forwardErr, startedAt)
			rec.CostUSD = costUSD
			rec.CostBreakdown = costBreakdown
			buffer.Enqueue(rec)
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
