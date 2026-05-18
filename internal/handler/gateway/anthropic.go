// Package gateway hosts the HTTP handlers that accept client requests
// and dispatch them through the Forwarder.
//
// MVP shape: one handler per upstream protocol. The handler runs a
// bounded retry loop on top of the Resolver + Forwarder: the upstream
// is contacted, and a retriable status code (5xx, 429) or transport
// error rolls over to a different account before any bytes reach the
// client. Once Forwarder.WriteResponse starts writing, the response is
// committed and any subsequent failure surfaces to the client.
package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/jami1024/omnihub/internal/service/health"
	"github.com/jami1024/omnihub/internal/service/limits"
	"github.com/jami1024/omnihub/internal/service/pricing"
	"github.com/jami1024/omnihub/internal/service/provider"
	"github.com/jami1024/omnihub/internal/service/resolver"
	"github.com/jami1024/omnihub/internal/service/session"
)

// anthropicCompatibleProviders is the allow-list for the Anthropic
// Messages endpoint. Both the direct API driver and Claude Platform
// on AWS accept the same wire format.
var anthropicCompatibleProviders = []string{"anthropic", "claude-platform"}

// clientMetadataHeaders is the allow-list of inbound HTTP headers
// the gateway forwards to compatible upstream LLMs. Limited to:
//
//   - Anthropic SDK identifiers (x-stainless-*, x-app)
//   - Conversation correlation IDs (x-claude-code-session-id,
//     x-client-request-id)
//
// These improve upstream cache partitioning and analytics without
// leaking PII (no IP, no User-Agent, no auth).
var clientMetadataHeaders = []string{
	"x-stainless-lang",
	"x-stainless-package-version",
	"x-stainless-os",
	"x-stainless-arch",
	"x-stainless-runtime",
	"x-stainless-runtime-version",
	"x-stainless-retry-count",
	"x-stainless-timeout",
	"x-stainless-helper-method",
	"x-app",
	"x-claude-code-session-id",
	"x-client-request-id",
}

// maxFailoverAttempts caps how many distinct accounts the retry loop
// will try for one inbound request before surfacing a 503.
const maxFailoverAttempts = 3

// AnthropicMessagesHandler returns a gin.HandlerFunc for the
// Anthropic-compatible POST /v1/messages endpoint.
//
// On each request:
//
//  1. Body is read once and decoded into IR.
//  2. The retry loop asks the resolver for an account (excluding the
//     IDs already tried this request), dispatches to it, and on a
//     retriable failure tries again with another account.
//  3. The first non-retriable response is committed to the client.
//  4. Health is recorded per attempt; persistence (when configured)
//     captures the final attempt.
//
// buffer may be nil (log-only mode without DB). tracker may be nil to
// disable health-aware filtering (every account is always considered
// available).
func AnthropicMessagesHandler(
	forwarder *forward.Forwarder,
	res resolver.Resolver,
	tracker *health.Tracker,
	buffer *repository.WriteBuffer,
	prices pricing.Table,
	limiter *limits.Limiter,
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

		if beta := c.GetHeader("anthropic-beta"); beta != "" && len(req.AnthropicBeta) == 0 {
			req.AnthropicBeta = splitCSV(beta)
		}

		// Collect SDK identifier headers (x-stainless-*, x-app, etc.)
		// for forwarding to compatible upstreams. See
		// clientMetadataHeaders for the allow-list.
		req.ClientMetadata = collectClientMetadata(c)

		c.Set(guard.CtxKeyModel, req.Model)
		c.Set(guard.CtxKeyStream, req.Stream)
		c.Set(guard.CtxKeyClientIP, c.ClientIP())
		c.Set(guard.CtxKeyUserAgent, c.GetHeader("User-Agent"))

		// Per-key policy: model allow-list + rolling 24h USD cap.
		// Runs after IR is parsed (we need req.Model) but before any
		// upstream call, so a capped key burns zero upstream quota.
		if limiter != nil {
			if rej := limiter.Check(c.Request.Context(), guard.APIKey(c), req.Model); rej != nil {
				errorJSON(c, rej.Status, rej.Type, rej.Message)
				return
			}
		}

		// Derive a session key so consecutive turns of the same
		// conversation hit the same upstream — Anthropic prompt
		// cache is per-account, so stickiness drives cost down.
		sessionKey := session.KeyFor(guard.KeyName(c), &req)

		// Retry loop: at most maxFailoverAttempts distinct accounts.
		var (
			attempted     []int64
			lastDriver    provider.Driver
			lastAccount   *provider.Account
			lastErr       error
			lastBadStatus int
			lastBody      []byte
		)

		for attempt := 0; attempt < maxFailoverAttempts; attempt++ {
			account, driver, rerr := res.ResolveForProviders(sessionKey, anthropicCompatibleProviders, attempted)
			if rerr != nil {
				if errors.Is(rerr, resolver.ErrNoUpstream) {
					if attempt == 0 {
						errorJSON(c, http.StatusServiceUnavailable, "no_upstream_available",
							"no upstream account is available for this request")
						recordNoUpstreamRejection(c, &req, buffer, startedAt)
						return
					}
					// Earlier attempts failed but we ran out of fresh
					// accounts. Surface the last upstream error.
					emitFailoverExhausted(c, lastBadStatus, lastErr)
					recordExhaustedFailure(c, &req, lastDriver, lastAccount, buffer, startedAt, lastBadStatus, lastErr, lastBody)
					return
				}
				slog.Error("resolver failed", "err", rerr.Error())
				errorJSON(c, http.StatusInternalServerError, "internal_error", "resolver failed")
				return
			}

			resp, sentAt, derr := forwarder.Dispatch(c.Request.Context(), &req, driver, account)
			if derr != nil {
				// Transport-level failure: always retriable.
				if tracker != nil {
					tracker.RecordFailure(account.ID, derr)
				}
				slog.Warn("upstream dispatch failed; trying next account",
					"account", account.Name, "attempt", attempt+1, "err", derr.Error())
				attempted = append(attempted, account.ID)
				lastDriver, lastAccount, lastErr, lastBadStatus = driver, account, derr, 0
				continue
			}

			if forward.IsRetriable(resp.StatusCode) {
				// Capture (cap at 8 KiB) before closing so the retry-
				// exhaustion record carries the upstream's actual
				// reply — for 429 that's the precise rate-limit message
				// and remaining-tokens info, otherwise invisible.
				lastBody, _ = io.ReadAll(io.LimitReader(resp.Body, 8<<10))
				_ = resp.Body.Close()
				if tracker != nil {
					tracker.RecordFailure(account.ID, fmt.Errorf("upstream HTTP %d", resp.StatusCode))
				}
				slog.Warn("upstream returned retriable status; trying next account",
					"account", account.Name, "attempt", attempt+1, "status", resp.StatusCode)
				attempted = append(attempted, account.ID)
				lastDriver, lastAccount, lastBadStatus = driver, account, resp.StatusCode
				lastErr = fmt.Errorf("upstream HTTP %d", resp.StatusCode)
				continue
			}

			// Commit: this response is what the client gets.
			result, writeErr := forwarder.WriteResponse(c.Writer, resp, &req, sentAt)
			recordHealthAfterWrite(tracker, account.ID, result, writeErr)

			c.Set(guard.CtxKeyUsage, result.Usage)
			if result.TTFB > 0 {
				c.Set(guard.CtxKeyTTFB, result.TTFB)
			}

			costUSD, costBreakdown := computeCost(prices, &req, &result, account)
			if costUSD != nil {
				c.Set(guard.CtxKeyCostUSD, *costUSD)
				// Fold the just-paid cost into the spend cache so the
				// next request from this key sees up-to-date data
				// without waiting for the WriteBuffer flush or the
				// cache TTL refresh.
				if limiter != nil {
					if k := guard.APIKey(c); k != nil {
						limiter.RecordSpend(k.Name, *costUSD)
					}
				}
			}

			if buffer != nil {
				rec := buildMessageRequest(c, &req, driver, account, &result, writeErr, startedAt)
				rec.CostUSD = costUSD
				rec.CostBreakdown = costBreakdown
				buffer.Enqueue(rec)
			}

			if writeErr != nil {
				slog.Error("forward failed after committing response",
					"account", account.Name,
					"status", result.StatusCode,
					"err", writeErr.Error())
			}
			return
		}

		// Loop body returns on every iteration. The only way to fall
		// out is exhausting maxFailoverAttempts with retriable errors.
		emitFailoverExhausted(c, lastBadStatus, lastErr)
		recordExhaustedFailure(c, &req, lastDriver, lastAccount, buffer, startedAt, lastBadStatus, lastErr, lastBody)
	}
}

// recordHealthAfterWrite updates the circuit breaker based on the
// committed response. 4xx (excluding 429) is a client problem and
// is NOT counted against the account.
func recordHealthAfterWrite(tracker *health.Tracker, accountID int64, result forward.Result, writeErr error) {
	if tracker == nil {
		return
	}
	if writeErr != nil && result.StatusCode == 0 {
		tracker.RecordFailure(accountID, writeErr)
		return
	}
	switch {
	case result.StatusCode >= 200 && result.StatusCode < 400:
		tracker.RecordSuccess(accountID)
	case result.StatusCode >= 500 || result.StatusCode == http.StatusTooManyRequests:
		tracker.RecordFailure(accountID, fmt.Errorf("upstream HTTP %d", result.StatusCode))
	}
}

// emitFailoverExhausted writes the final error envelope when every
// allowed account returned a retriable failure.
func emitFailoverExhausted(c *gin.Context, status int, lastErr error) {
	message := "all candidate upstream accounts failed after retries"
	if lastErr != nil {
		message = fmt.Sprintf("%s: %s", message, lastErr.Error())
	}
	httpStatus := http.StatusBadGateway
	if status == http.StatusTooManyRequests {
		httpStatus = http.StatusTooManyRequests
	}
	errorJSON(c, httpStatus, "all_upstreams_failed", message)
}

// recordNoUpstreamRejection persists a row when the resolver rejects
// the request before any upstream call — typically because the only
// available account is in circuit-open state. Without this, 503s
// from circuit-breaker tripping are invisible to message_requests
// and only show up in the log stream, making post-mortem analysis
// of outage windows harder than it needs to be.
//
// provider_name / account_name are NOT NULL in the schema; we use
// "-" as a synthetic placeholder so the row carries the same client
// identity (key, ip, ua) as a normal request would.
func recordNoUpstreamRejection(
	c *gin.Context,
	req *ir.UnifiedRequest,
	buffer *repository.WriteBuffer,
	startedAt time.Time,
) {
	if buffer == nil {
		return
	}
	duration := time.Since(startedAt).Milliseconds()
	rec := repository.MessageRequest{
		CreatedAt:    startedAt,
		Method:       c.Request.Method,
		Path:         c.Request.URL.Path,
		Model:        req.Model,
		Stream:       req.Stream,
		ProviderName: "-",
		AccountName:  "-",
		StatusCode:   intPtr(http.StatusServiceUnavailable),
		DurationMs:   int64Ptr(duration),
		ErrorMessage: strPtr("no upstream account available"),
	}
	if name := guard.KeyName(c); name != "" {
		rec.KeyName = strPtr(name)
	}
	if ip := guard.ClientIP(c); ip != "" {
		rec.ClientIP = strPtr(ip)
	}
	if ua := guard.UserAgent(c); ua != "" {
		rec.UserAgent = strPtr(ua)
	}
	buffer.Enqueue(rec)
}

// recordExhaustedFailure logs the final attempt into message_requests
// when the retry loop runs out of accounts. The captured upstream
// body (e.g. Anthropic's precise rate-limit message for 429) is
// surfaced via Result.ErrorBody so buildMessageRequest stores it in
// the error_message column rather than the generic "upstream HTTP N"
// string.
func recordExhaustedFailure(
	c *gin.Context,
	req *ir.UnifiedRequest,
	driver provider.Driver,
	account *provider.Account,
	buffer *repository.WriteBuffer,
	startedAt time.Time,
	status int,
	err error,
	body []byte,
) {
	if buffer == nil || account == nil || driver == nil {
		return
	}
	result := forward.Result{StatusCode: status, ErrorBody: body}
	rec := buildMessageRequest(c, req, driver, account, &result, err, startedAt)
	buffer.Enqueue(rec)
}

// computeCost mirrors the previous handler logic, factored out to
// keep the retry loop body short.
func computeCost(
	prices pricing.Table,
	req *ir.UnifiedRequest,
	result *forward.Result,
	account *provider.Account,
) (*float64, *pricing.Breakdown) {
	modelForPricing := result.Usage.ActualModel
	if modelForPricing == "" {
		modelForPricing = req.Model
	}
	if prices == nil || modelForPricing == "" {
		return nil, nil
	}
	base, ok := prices.Calculate(modelForPricing, result.Usage)
	if !ok {
		slog.Warn("no pricing entry for model",
			"requested_model", req.Model,
			"actual_model", result.Usage.ActualModel,
		)
		return nil, nil
	}
	final := base.ApplyMultiplier(account.CostMultiplier)
	return &final.Total, &final
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
	if ip := guard.ClientIP(c); ip != "" {
		rec.ClientIP = strPtr(ip)
	}
	if ua := guard.UserAgent(c); ua != "" {
		rec.UserAgent = strPtr(ua)
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
	if msg := errorMessageFor(result, forwardErr); msg != "" {
		rec.ErrorMessage = strPtr(msg)
	}
	return rec
}

// errorMessageFor picks the most informative error string for a
// message_requests row. Upstream non-2xx responses get the captured
// upstream body (real diagnostic value: "prompt is too long", model
// blocked, etc.); pure transport / write failures fall back to the
// Go error string.
func errorMessageFor(result *forward.Result, forwardErr error) string {
	if result != nil && result.StatusCode >= 400 && len(result.ErrorBody) > 0 {
		return string(result.ErrorBody)
	}
	if forwardErr != nil {
		return forwardErr.Error()
	}
	return ""
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

// collectClientMetadata reads the allow-listed SDK identifier
// headers from the inbound request. Empty values are skipped so the
// driver never emits "x-stainless-lang:" with no value.
func collectClientMetadata(c *gin.Context) map[string]string {
	out := make(map[string]string, len(clientMetadataHeaders))
	for _, h := range clientMetadataHeaders {
		if v := c.GetHeader(h); v != "" {
			out[h] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
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
