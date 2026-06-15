package gateway

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	protoopenai "github.com/jami1024/omnihub/internal/protocol/openai"
	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/blockedip"
	"github.com/jami1024/omnihub/internal/service/forward"
	"github.com/jami1024/omnihub/internal/service/guard"
	"github.com/jami1024/omnihub/internal/service/health"
	"github.com/jami1024/omnihub/internal/service/limits"
	"github.com/jami1024/omnihub/internal/service/metrics"
	"github.com/jami1024/omnihub/internal/service/pricing"
	"github.com/jami1024/omnihub/internal/service/provider"
	"github.com/jami1024/omnihub/internal/service/resolver"
	"github.com/jami1024/omnihub/internal/service/session"
	"github.com/jami1024/omnihub/internal/service/usage"
)

// responsesCompatibleProviders is the allow-list for the OpenAI
// Responses endpoint. Only the experimental openai-codex driver speaks
// this wire format today.
var responsesCompatibleProviders = []string{"openai-codex"}

// responsesSessionKey derives the sticky-session key for /v1/responses
// per the §11.2 priority order. The Responses body is opaque to the
// gateway (pass-through), so unlike session.KeyFor there is no content
// digest to fall back on: an explicit X-OmniHub-Session-ID header wins,
// then the body's own conversation identifier (prompt_cache_key /
// session_id / conversation_id, lifted at parse time). No identifier →
// no stickiness.
func responsesSessionKey(c *gin.Context, virtualKey, model, affinity string) string {
	if sid := strings.TrimSpace(c.GetHeader(omnihubSessionHeader)); sid != "" {
		affinity = sid
	}
	return session.KeyForExplicit(virtualKey, model, affinity)
}

// ResponsesHandler returns a gin.HandlerFunc for the OpenAI-compatible
// POST /v1/responses endpoint (EXPERIMENTAL — Codex subscription
// accounts).
//
// It mirrors OpenAIChatCompletionsHandler's retry loop but:
//
//   - parses the inbound body minimally (model/stream/affinity) and
//     preserves it verbatim for the codex driver — matched-pair
//     pass-through, no IR re-rendering;
//   - routes only to openai-codex accounts;
//   - extracts usage with the Responses parser;
//   - binds sticky sessions on the client's prompt_cache_key.
func ResponsesHandler(
	forwarder *forward.Forwarder,
	res resolver.Resolver,
	tracker *health.Tracker,
	buffer *repository.WriteBuffer,
	prices pricing.Calculator,
	limiter *limits.Limiter,
	blockedIPs *blockedip.Pool,
	charger BillingCharger,
	tokens TokenFreshener,
	conc *limits.ConcurrencyGuard,
	authGuard AuthGuard,
	settings ...RuntimeSettings,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		startedAt := time.Now()
		maxAttempts := configuredFailoverAttempts(settings)

		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			openaiErrorJSON(c, http.StatusBadRequest, "invalid_request_error", "read body: "+err.Error())
			return
		}

		req, affinity, perr := protoopenai.RequestFromResponses(body)
		if perr != nil {
			openaiErrorJSON(c, http.StatusBadRequest, "invalid_request_error", perr.Error())
			return
		}

		req.ClientMetadata = collectClientMetadata(c)
		req.ClientIP = c.ClientIP()

		c.Set(guard.CtxKeyModel, req.Model)
		c.Set(guard.CtxKeyStream, req.Stream)
		c.Set(guard.CtxKeyClientIP, c.ClientIP())
		c.Set(guard.CtxKeyUserAgent, c.GetHeader("User-Agent"))

		if limiter != nil {
			if rej := limiter.Check(c.Request.Context(), guard.APIKey(c), req.Model); rej != nil {
				openaiErrorJSON(c, rej.Status, rej.Type, rej.Message)
				return
			}
		}

		sessionKey := responsesSessionKey(c, guard.KeyName(c), req.Model, affinity)

		var (
			attempted     []int64
			lastDriver    provider.Driver
			lastAccount   *provider.Account
			lastErr       error
			lastBadStatus int
			lastBody      []byte
			authRefreshed bool
		)

		for attempt := 0; attempt < maxAttempts; attempt++ {
			account, driver, rerr := res.ResolveForProviders(sessionKey, responsesCompatibleProviders, attempted)
			if rerr != nil {
				if errors.Is(rerr, resolver.ErrNoUpstream) {
					if attempt == 0 {
						openaiErrorJSON(c, http.StatusServiceUnavailable, "no_upstream_available",
							"no upstream account is available for this request")
						recordNoUpstreamRejection(c, req, buffer, startedAt)
						return
					}
					openaiEmitFailoverExhausted(c, lastBadStatus, lastErr)
					recordExhaustedFailure(c, req, lastDriver, lastAccount, buffer, startedAt, lastBadStatus, lastErr, lastBody)
					return
				}
				slog.Error("resolver failed", "err", rerr.Error())
				openaiErrorJSON(c, http.StatusInternalServerError, "internal_error", "resolver failed")
				return
			}

			// Codex accounts are always OAuth-backed: refresh the access
			// token when it is inside the expiry window.
			if fresh, ferr := ensureFreshAccount(c.Request.Context(), tokens, account); ferr != nil {
				slog.Warn("token refresh failed; trying next account",
					"account", account.Name, "attempt", attempt+1, "err", ferr.Error())
				attempted = append(attempted, account.ID)
				metrics.IncFailover(driver.Name())
				lastDriver, lastAccount, lastErr, lastBadStatus = driver, account, ferr, 0
				continue
			} else {
				account = fresh
			}

			// Per-account concurrency cap (see AnthropicMessagesHandler).
			if conc != nil {
				if !conc.TryAcquire(account.ID, account.MaxConcurrency) {
					slog.Warn("account at concurrency cap; trying next account",
						"account", account.Name, "attempt", attempt+1)
					attempted = append(attempted, account.ID)
					continue
				}
				defer conc.Release(account.ID)
			}

			resp, sentAt, derr := forwarder.Dispatch(c.Request.Context(), req, driver, account)
			if derr != nil {
				if tracker != nil {
					tracker.RecordFailure(account.ID, derr)
				}
				slog.Warn("upstream dispatch failed; trying next account",
					"account", account.Name, "attempt", attempt+1, "err", derr.Error())
				attempted = append(attempted, account.ID)
				metrics.IncFailover(driver.Name())
				lastDriver, lastAccount, lastErr, lastBadStatus = driver, account, derr, 0
				continue
			}

			if forward.IsRetriable(resp.StatusCode) {
				lastBody, _ = io.ReadAll(io.LimitReader(resp.Body, 8<<10))
				_ = resp.Body.Close()
				applyRateLimitCooldown(tracker, account.ID, resp, lastBody)
				if tracker != nil {
					tracker.RecordFailure(account.ID, fmt.Errorf("upstream HTTP %d", resp.StatusCode))
				}
				slog.Warn("upstream returned retriable status; trying next account",
					"account", account.Name, "attempt", attempt+1, "status", resp.StatusCode)
				attempted = append(attempted, account.ID)
				metrics.IncFailover(driver.Name())
				lastDriver, lastAccount, lastBadStatus = driver, account, resp.StatusCode
				lastErr = fmt.Errorf("upstream HTTP %d", resp.StatusCode)
				continue
			}

			// 401: force one token refresh and retry the SAME account
			// before failing over (mirrors the other handlers).
			if resp.StatusCode == http.StatusUnauthorized && tokens != nil && account.UsesUpstreamOAuth() && !authRefreshed {
				authRefreshed = true
				_ = resp.Body.Close()
				refreshed, rerr := tokens.ForceRefresh(c.Request.Context(), account.ID)
				if rerr != nil {
					slog.Warn("401 force-refresh failed; trying next account",
						"account", account.Name, "attempt", attempt+1, "err", rerr.Error())
					attempted = append(attempted, account.ID)
					metrics.IncFailover(driver.Name())
					lastDriver, lastAccount, lastBadStatus = driver, account, resp.StatusCode
					lastErr = fmt.Errorf("upstream HTTP 401 and token refresh failed: %w", rerr)
					continue
				}
				account = refreshed
				slog.Info("401 recovered by token refresh; retrying same account", "account", account.Name)
				retryResp, retrySentAt, retryErr := forwarder.Dispatch(c.Request.Context(), req, driver, account)
				if retryErr != nil {
					if tracker != nil {
						tracker.RecordFailure(account.ID, retryErr)
					}
					attempted = append(attempted, account.ID)
					metrics.IncFailover(driver.Name())
					lastDriver, lastAccount, lastErr, lastBadStatus = driver, account, retryErr, 0
					continue
				}
				if forward.IsRetriable(retryResp.StatusCode) {
					lastBody, _ = io.ReadAll(io.LimitReader(retryResp.Body, 8<<10))
					_ = retryResp.Body.Close()
					applyRateLimitCooldown(tracker, account.ID, retryResp, lastBody)
					if tracker != nil {
						tracker.RecordFailure(account.ID, fmt.Errorf("upstream HTTP %d", retryResp.StatusCode))
					}
					attempted = append(attempted, account.ID)
					metrics.IncFailover(driver.Name())
					lastDriver, lastAccount, lastBadStatus = driver, account, retryResp.StatusCode
					lastErr = fmt.Errorf("upstream HTTP %d", retryResp.StatusCode)
					continue
				}
				resp = retryResp
				sentAt = retrySentAt
			}

			// Commit: pass-through of the Responses bytes (JSON or SSE);
			// the Responses sniffer pulls usage from response.completed.
			result, writeErr := forwarder.WriteResponse(c.Writer, resp, req, sentAt, usage.Responses)
			recordHealthAfterWrite(tracker, account.ID, result, writeErr)
			if authGuard != nil {
				authGuard.Record(c.Request.Context(), account, result.StatusCode)
			}

			c.Set(guard.CtxKeyUsage, result.Usage)
			if result.TTFB > 0 {
				c.Set(guard.CtxKeyTTFB, result.TTFB)
			}

			costUSD, costBreakdown := computeCost(prices, req, &result, account)
			if costUSD != nil {
				c.Set(guard.CtxKeyCostUSD, *costUSD)
				if limiter != nil {
					if k := guard.APIKey(c); k != nil {
						limiter.RecordSpend(k, *costUSD)
					}
				}
			}

			emitMetrics(driver.Name(), req.Model, &result, costUSD, startedAt)

			chargeIPTokenBudget(c, blockedIPs, &result)

			if buffer != nil {
				rec := buildMessageRequest(c, req, driver, account, &result, writeErr, startedAt)
				rec.CostUSD = costUSD
				if k := guard.APIKey(c); k != nil && k.UserID != nil && costUSD != nil && *costUSD > 0 {
					split := chargeBilling(c.Request.Context(), k, costUSD, charger, limiter)
					rec.BilledUSD = &split.BilledUSD
					rec.PlanBilledUSD = floatPtrIfPositive(split.PlanUSD)
					rec.WalletBilledUSD = &split.WalletUSD
					rec.PlanGrantID = split.PlanGrantID
				}
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

		openaiEmitFailoverExhausted(c, lastBadStatus, lastErr)
		recordExhaustedFailure(c, req, lastDriver, lastAccount, buffer, startedAt, lastBadStatus, lastErr, lastBody)
	}
}

// ModelsHandler returns a gin.HandlerFunc for GET /v1/models: a static
// OpenAI-shaped model list. The codex backend exposes no model-list
// endpoint, so the gateway serves the known set (account model
// redirects can map anything else onto it).
func ModelsHandler(modelIDs []string) gin.HandlerFunc {
	type modelEntry struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		OwnedBy string `json:"owned_by"`
	}
	return func(c *gin.Context) {
		data := make([]modelEntry, 0, len(modelIDs))
		for _, id := range modelIDs {
			data = append(data, modelEntry{ID: id, Object: "model", OwnedBy: "system"})
		}
		c.JSON(http.StatusOK, gin.H{"object": "list", "data": data})
	}
}
