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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/ir"
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

// anthropicCompatibleProviders is the allow-list for the Anthropic
// Messages endpoint. The direct API driver, Claude Platform on AWS and
// the experimental Claude subscription driver all accept the same wire
// format.
var anthropicCompatibleProviders = []string{"anthropic", "claude-platform", "claude-subscription"}

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

// RuntimeSettings exposes hot-path gateway knobs that may be changed from the
// admin UI. Nil settings fall back to the historical constants.
type RuntimeSettings interface {
	FailoverMaxAttempts() int
}

// AuthGuard parks accounts that persistently fail upstream auth (a
// revoked/expired api_key that 401s every request). *authguard.Guard
// implements it; nil disables the feature.
type AuthGuard interface {
	Record(ctx context.Context, account *provider.Account, status int)
}

// TokenFreshener keeps OAuth-backed accounts' upstream tokens fresh.
// *upstreamauth.TokenManager implements it; nil disables refresh (all
// accounts are treated as static-credential).
type TokenFreshener interface {
	// EnsureFresh returns an account whose token has enough life left
	// for this request, refreshing it first when needed.
	EnsureFresh(ctx context.Context, a *provider.Account) (*provider.Account, error)
	// ForceRefresh renews the token after an upstream 401.
	ForceRefresh(ctx context.Context, accountID int64) (*provider.Account, error)
}

// omnihubSessionHeader is the gateway's own client-supplied session
// identifier (design doc §10). It is consumed here for sticky routing
// and never reaches the upstream: drivers build outbound requests from
// scratch and only copy the ClientMetadata allow-list, which does not
// include X-OmniHub-*.
const omnihubSessionHeader = "X-OmniHub-Session-ID"

// sessionKeyFor implements the SessionHash priority order from the
// design doc §11.2: an explicit X-OmniHub-Session-ID header wins, then
// the request's metadata.user_id hint, then the content-digest
// fallback (system + first user message).
func sessionKeyFor(c *gin.Context, virtualKey string, req *ir.UnifiedRequest) string {
	if sid := strings.TrimSpace(c.GetHeader(omnihubSessionHeader)); sid != "" {
		return session.KeyForExplicit(virtualKey, req.Model, sid)
	}
	if uid, ok := req.Metadata["user_id"].(string); ok && uid != "" {
		return session.KeyForExplicit(virtualKey, req.Model, "user:"+uid)
	}
	return session.KeyFor(virtualKey, req)
}

// rate-limit cooldown bounds: a 429 with no usable Retry-After parks
// the account for the default; an absurdly long upstream value is
// capped so a clock-skewed header cannot bench an account for hours.
const (
	defaultRateLimitCooldown = time.Minute
	maxRateLimitCooldown     = 30 * time.Minute
)

// applyRateLimitCooldown parks a 429'd account until the upstream says
// capacity returns. The reset signal is taken from the first source
// that yields a usable value (see rateLimitCooldown): the standard
// Retry-After header, Anthropic's OAuth ratelimit-reset headers, the
// codex backend's reset-after header, or a reset hint in the error
// body. body may be nil (header-only). No-op for non-429 statuses.
func applyRateLimitCooldown(tracker *health.Tracker, accountID int64, resp *http.Response, body []byte) {
	if tracker == nil || resp == nil || resp.StatusCode != http.StatusTooManyRequests {
		return
	}
	d, ok := rateLimitCooldown(resp.Header, body)
	if !ok || d < time.Second {
		d = defaultRateLimitCooldown
	}
	if d > maxRateLimitCooldown {
		d = maxRateLimitCooldown
	}
	tracker.SetCooldown(accountID, time.Now().Add(d))
}

// rateLimitCooldown derives how long to park a rate-limited account
// from the upstream's reset signals, in priority order. Returns
// (0, false) when no source yields a positive future reset.
func rateLimitCooldown(h http.Header, body []byte) (time.Duration, bool) {
	// 1) Retry-After: relative seconds or an HTTP-date. The most
	//    explicit and standard signal, so it wins.
	if ra := strings.TrimSpace(h.Get("Retry-After")); ra != "" {
		if secs, err := strconv.Atoi(ra); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second, true
		}
		if at, err := http.ParseTime(ra); err == nil {
			if until := time.Until(at); until > 0 {
				return until, true
			}
		}
	}
	// 2) Anthropic OAuth ratelimit-reset headers (absolute time —
	//    RFC3339 or unix seconds). Prefer the unified field; otherwise
	//    take the SOONEST window reset so the account is not benched
	//    longer than necessary (a later 429 re-parks it).
	if d, ok := parseAbsReset(h.Get("anthropic-ratelimit-unified-reset")); ok {
		return d, true
	}
	var best time.Duration
	found := false
	for _, k := range []string{"anthropic-ratelimit-unified-5h-reset", "anthropic-ratelimit-unified-7d-reset"} {
		if d, ok := parseAbsReset(h.Get(k)); ok && (!found || d < best) {
			best, found = d, true
		}
	}
	if found {
		return best, true
	}
	// 3) Codex backend reset-after header (relative seconds).
	if ra := strings.TrimSpace(h.Get("x-codex-primary-reset-after-seconds")); ra != "" {
		if secs, err := strconv.Atoi(ra); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second, true
		}
	}
	// 4) Reset hint in the error body (codex usage_limit_reached /
	//    claude rate-limit errors carry resets_in_seconds / resets_at).
	return rateLimitBodyReset(body)
}

// parseAbsReset parses an absolute reset timestamp — RFC3339 or unix
// seconds — into the remaining duration until it. Returns (0, false)
// for empty, unparseable, or already-elapsed values.
func parseAbsReset(s string) (time.Duration, bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "null" {
		return 0, false
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		if until := time.Until(t); until > 0 {
			return until, true
		}
		return 0, false
	}
	if sec, err := strconv.ParseInt(s, 10, 64); err == nil && sec > 0 {
		if until := time.Until(time.Unix(sec, 0)); until > 0 {
			return until, true
		}
	}
	return 0, false
}

// rateLimitBodyReset extracts a reset hint from a (truncated) error
// body. Handles both the top-level and error-nested shapes used by the
// codex (usage_limit_reached) and claude rate-limit responses:
// resets_in_seconds (relative) or resets_at (absolute unix / RFC3339).
func rateLimitBodyReset(body []byte) (time.Duration, bool) {
	if len(body) == 0 {
		return 0, false
	}
	var p struct {
		ResetsInSeconds *int64          `json:"resets_in_seconds"`
		ResetsAt        json.RawMessage `json:"resets_at"`
		Error           struct {
			ResetsInSeconds *int64          `json:"resets_in_seconds"`
			ResetsAt        json.RawMessage `json:"resets_at"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &p) != nil {
		return 0, false
	}
	for _, s := range []*int64{p.ResetsInSeconds, p.Error.ResetsInSeconds} {
		if s != nil && *s > 0 {
			return time.Duration(*s) * time.Second, true
		}
	}
	for _, raw := range []json.RawMessage{p.ResetsAt, p.Error.ResetsAt} {
		if d, ok := parseAbsReset(strings.Trim(string(raw), `"`)); ok {
			return d, true
		}
	}
	return 0, false
}

// ensureFreshAccount runs the TokenManager gate for one resolved
// account. Non-OAuth accounts and a nil freshener pass through.
func ensureFreshAccount(ctx context.Context, tokens TokenFreshener, account *provider.Account) (*provider.Account, error) {
	if tokens == nil || !account.UsesUpstreamOAuth() {
		return account, nil
	}
	return tokens.EnsureFresh(ctx, account)
}

func configuredFailoverAttempts(settings []RuntimeSettings) int {
	if len(settings) == 0 || settings[0] == nil {
		return maxFailoverAttempts
	}
	n := settings[0].FailoverMaxAttempts()
	if n < 1 {
		return 1
	}
	if n > 10 {
		return 10
	}
	return n
}

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
		// Resolved real client IP; only emitted upstream when the chosen
		// account sets ForwardClientIP (the forwarder decides).
		req.ClientIP = c.ClientIP()

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
		sessionKey := sessionKeyFor(c, guard.KeyName(c), &req)

		// Retry loop: at most maxFailoverAttempts distinct accounts.
		var (
			attempted          []int64
			lastDriver         provider.Driver
			lastAccount        *provider.Account
			lastErr            error
			lastBadStatus      int
			lastBody           []byte
			signatureRectified bool
			authRefreshed      bool
		)

		for attempt := 0; attempt < maxAttempts; attempt++ {
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

			// OAuth-backed accounts: make sure the access token has
			// enough life left before spending an attempt on it. A
			// failed refresh parks the account (the resolver will skip
			// it) and we fail over without touching the upstream.
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

			// Per-account concurrency cap: reserve an in-flight slot
			// before dispatching. The deferred release fires when the
			// handler returns — on failover a slot is held marginally
			// longer than its dispatch, but it can never leak.
			if conc != nil {
				if !conc.TryAcquire(account.ID, account.MaxConcurrency) {
					slog.Warn("account at concurrency cap; trying next account",
						"account", account.Name, "attempt", attempt+1)
					attempted = append(attempted, account.ID)
					continue
				}
				defer conc.Release(account.ID)
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
				metrics.IncFailover(driver.Name())
				lastDriver, lastAccount, lastErr, lastBadStatus = driver, account, derr, 0
				continue
			}

			if forward.IsRetriable(resp.StatusCode) {
				// Capture (cap at 8 KiB) before closing so the retry-
				// exhaustion record carries the upstream's actual
				// reply — for 429 that's the precise rate-limit message
				// and remaining-tokens info, otherwise invisible. The
				// same body feeds the cooldown's reset-hint parsing.
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

			// 401 from an OAuth-backed upstream usually means the access
			// token expired mid-flight or was rotated by another gateway
			// instance. Force one refresh and retry the SAME account
			// before failing over; api_key accounts keep the historical
			// behaviour (401 commits to the client).
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
				retryResp, retrySentAt, retryErr := forwarder.Dispatch(c.Request.Context(), &req, driver, account)
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
					// The refreshed retry hit a 5xx/429 — hand it to the
					// normal failover path instead of committing it.
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

			// Thinking-signature rectifier: when the upstream rejects
			// the request because a replayed thinking block carries
			// an unverifiable signature, downgrade the offending
			// blocks to plain text and retry the SAME account before
			// committing the failure. Runs at most once per request.
			if resp.StatusCode == 400 && !signatureRectified {
				peek, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
				_ = resp.Body.Close()
				if forward.IsThinkingSignatureError(peek) {
					rectified := ir.RectifyThinkingBlocks(&req)
					slog.Info("thinking signature mismatch; retrying with rectified request",
						"account", account.Name)
					retryResp, retrySentAt, retryErr := forwarder.Dispatch(c.Request.Context(), rectified, driver, account)
					if retryErr != nil {
						slog.Warn("rectified retry failed at transport; surfacing original error",
							"account", account.Name, "err", retryErr.Error())
						resp.Body = io.NopCloser(bytes.NewReader(peek))
					} else {
						req = *rectified
						resp = retryResp
						sentAt = retrySentAt
						signatureRectified = true
					}
				} else {
					// Not a signature mismatch — replay the buffered
					// body to WriteResponse unchanged.
					resp.Body = io.NopCloser(bytes.NewReader(peek))
				}
			}

			// Commit: this response is what the client gets.
			result, writeErr := forwarder.WriteResponse(c.Writer, resp, &req, sentAt, usage.Anthropic)
			recordHealthAfterWrite(tracker, account.ID, result, writeErr)
			if authGuard != nil {
				authGuard.Record(c.Request.Context(), account, result.StatusCode)
			}

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
						limiter.RecordSpend(k, *costUSD)
					}
				}
			}

			emitMetrics(driver.Name(), req.Model, &result, costUSD, startedAt)

			// Charge the per-IP TPM bucket for the fresh-input tokens
			// this request actually consumed. The middleware admitted
			// the request based on the pre-existing budget; this is
			// the matching post-flight deduction so the next request
			// from the same IP sees up-to-date capacity.
			chargeIPTokenBudget(c, blockedIPs, &result)

			if buffer != nil {
				rec := buildMessageRequest(c, &req, driver, account, &result, writeErr, startedAt)
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
	emitMetrics("-", req.Model, &forward.Result{StatusCode: http.StatusServiceUnavailable}, nil, startedAt)

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
	if sid := sessionIDFromRequest(c, req); sid != "" {
		rec.SessionID = strPtr(sid)
	}
	buffer.Enqueue(rec)
}

// recordExhaustedFailure logs the final attempt into message_requests
// when the retry loop runs out of accounts. The captured upstream
// body (e.g. Anthropic's precise rate-limit message for 429) is
// surfaced via Result.ErrorBody so buildMessageRequest stores it in
// the error_message column rather than the generic "upstream HTTP N"
// string.
// emitMetrics folds one committed/terminal response into the Prometheus
// gateway metrics. It is deliberately independent of the WriteBuffer so
// metrics are emitted even in log-only mode. costUSD is nil when the
// model is unpriced; result may carry a zero TTFB / usage for failures.
func emitMetrics(providerName, requestedModel string, result *forward.Result, costUSD *float64, startedAt time.Time) {
	model := requestedModel
	cost := 0.0
	var (
		status int
		ttfb   time.Duration
		inTok  int64
		outTok int64
	)
	if result != nil {
		status = result.StatusCode
		ttfb = result.TTFB
		inTok = result.Usage.InputTokens
		outTok = result.Usage.OutputTokens
		if result.Usage.ActualModel != "" {
			model = result.Usage.ActualModel
		}
	}
	if costUSD != nil {
		cost = *costUSD
	}
	metrics.Record(metrics.Sample{
		Provider:     providerName,
		Model:        model,
		Status:       status,
		Duration:     time.Since(startedAt),
		TTFB:         ttfb,
		InputTokens:  inTok,
		OutputTokens: outTok,
		CostUSD:      cost,
	})
}

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
	providerName := "unknown"
	if driver != nil {
		providerName = driver.Name()
	}
	result := forward.Result{StatusCode: status, ErrorBody: body}
	emitMetrics(providerName, req.Model, &result, nil, startedAt)

	if buffer == nil || account == nil || driver == nil {
		return
	}
	rec := buildMessageRequest(c, req, driver, account, &result, err, startedAt)
	buffer.Enqueue(rec)
}

// computeCost mirrors the previous handler logic, factored out to
// keep the retry loop body short.
func computeCost(
	prices pricing.Calculator,
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
	final := base.ApplyMultiplier(account.EffectiveCostMultiplier())
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
	if sid := sessionIDFromRequest(c, req); sid != "" {
		rec.SessionID = strPtr(sid)
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

// chargeIPTokenBudget deducts the fresh-input tokens of the just-
// completed request from the per-IP TPM bucket. No-op when no
// policy / no TPM cap applies. "Fresh input" = input + cache_create
// — matching what Anthropic itself counts against ITPM. cache_read
// tokens are excluded so heavy cache reuse doesn't burn the budget.
func chargeIPTokenBudget(c *gin.Context, pool *blockedip.Pool, result *forward.Result) {
	if pool == nil || result == nil {
		return
	}
	policy := guard.IPPolicy(c)
	if policy == nil || policy.TPMLimit <= 0 {
		return
	}
	fresh := result.Usage.InputTokens + result.Usage.CacheCreationInputTokens
	if fresh <= 0 {
		return
	}
	pool.TPMBucket().Charge(c.ClientIP(), policy.TPMLimit, fresh)
}

// sessionIDFromRequest pulls the Claude Code session correlation id
// from the inbound request. The header is already collected into
// req.ClientMetadata by the handler entry — read from there to keep
// one source of truth — but fall back to the raw header so the
// no-upstream path (which never gets to set ClientMetadata for
// every code path) still attributes correctly.
func sessionIDFromRequest(c *gin.Context, req *ir.UnifiedRequest) string {
	const header = "x-claude-code-session-id"
	if req != nil {
		if v, ok := req.ClientMetadata[header]; ok && v != "" {
			return v
		}
	}
	return c.GetHeader(header)
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
