package gateway

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
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
	"github.com/jami1024/omnihub/internal/service/usage"
)

// openaiCompatibleProviders is the allow-list for the OpenAI Chat
// Completions endpoint. Only the openai driver (OpenAI and any
// OpenAI-compatible upstream) speaks this wire format.
var openaiCompatibleProviders = []string{"openai"}

// OpenAIChatCompletionsHandler returns a gin.HandlerFunc for the
// OpenAI-compatible POST /v1/chat/completions endpoint.
//
// It mirrors AnthropicMessagesHandler's retry loop and reuses the same
// resolver, limiter, pricing, and persistence helpers, but:
//
//   - parses the inbound body as OpenAI (RequestFromOpenAI → IR);
//   - routes only to openai-family accounts;
//   - extracts usage with the OpenAI parser;
//   - emits OpenAI-shaped error envelopes;
//   - skips the Anthropic thinking-signature rectifier.
//
// On the matched-pair path the upstream also speaks OpenAI, so the
// response is forwarded to the client verbatim.
func OpenAIChatCompletionsHandler(
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

		req, err := protoopenai.RequestFromOpenAI(body)
		if err != nil {
			openaiErrorJSON(c, http.StatusBadRequest, "invalid_request_error", err.Error())
			return
		}

		// SDK identifier headers (x-stainless-*, x-app) are emitted by
		// the OpenAI SDK family too; forward the allow-listed set.
		req.ClientMetadata = collectClientMetadata(c)
		// Resolved real client IP; only emitted upstream when the chosen
		// account sets ForwardClientIP (the forwarder decides).
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

		sessionKey := sessionKeyFor(c, guard.KeyName(c), req)

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
			account, driver, rerr := res.ResolveForProviders(sessionKey, openaiCompatibleProviders, attempted)
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

			// OAuth-backed accounts: refresh the access token first when
			// it is inside the expiry window (mirrors the Anthropic
			// handler; see ensureFreshAccount).
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

			// 401 from an OAuth-backed upstream: force one token refresh
			// and retry the SAME account before failing over (mirrors the
			// Anthropic handler). api_key accounts commit the 401 as-is.
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

			// Commit: this response is what the client gets. Matched-pair
			// pass-through means the OpenAI upstream bytes reach the
			// OpenAI client unchanged; usage.OpenAI sniffs token counts.
			result, writeErr := forwarder.WriteResponse(c.Writer, resp, req, sentAt, usage.OpenAI)
			recordHealthAfterWrite(tracker, account.ID, result, writeErr)

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
					// wallet_billed_usd is stored explicitly (even 0) so a
					// plan-covered request is not re-counted as wallet spend
					// via SumBilledByUser's legacy cost fallback.
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

// openaiErrorJSON writes an OpenAI-shaped error envelope. OpenAI SDKs
// parse { "error": { message, type, param, code } }.
func openaiErrorJSON(c *gin.Context, status int, errType, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"message": message,
			"type":    errType,
			"param":   nil,
			"code":    errType,
		},
	})
}

// openaiEmitFailoverExhausted writes the final OpenAI error envelope when
// every allowed account returned a retriable failure.
func openaiEmitFailoverExhausted(c *gin.Context, status int, lastErr error) {
	message := "all candidate upstream accounts failed after retries"
	if lastErr != nil {
		message = fmt.Sprintf("%s: %s", message, lastErr.Error())
	}
	httpStatus := http.StatusBadGateway
	if status == http.StatusTooManyRequests {
		httpStatus = http.StatusTooManyRequests
	}
	openaiErrorJSON(c, httpStatus, "all_upstreams_failed", message)
}
