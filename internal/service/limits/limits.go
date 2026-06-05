// Package limits enforces per-key spend and model policies after
// authentication has identified the caller. It owns two checks:
//
//   - Model allow-list (api_keys.allowed_models): synchronous,
//     in-memory, zero IO. A non-empty allow-list rejects requests
//     whose model is not present.
//   - Daily USD cap (api_keys.daily_usd_limit): rolling 24h window of
//     summed cost_usd from message_requests, served by SpendCache so
//     hot keys do not hit the DB on every request.
//
// RPM and concurrency caps live elsewhere — they need their own
// clock/state machine and don't share the limits.Check signature.
package limits

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jami1024/omnihub/internal/service/apikey"
)

// Reject is the structured rejection returned by Check when a request
// must be denied. The handler turns it into an HTTP error envelope.
type Reject struct {
	Status  int    // HTTP status code (403 / 429)
	Type    string // Anthropic-shaped error.type
	Message string // human-readable detail
}

// Error implements error so callers may use errors.Is-style flow if
// desired; the field is otherwise the source of truth.
func (r *Reject) Error() string { return r.Message }

// Limiter holds the policy machinery. Construct one via New and
// share it across the gateway — the caches inside are safe for
// concurrent use.
type Limiter struct {
	cache   *SpendCache
	rpm     *RPMCache
	balance *BalanceGuard // nil = prepaid-balance billing disabled
}

// SetBalanceGuard enables prepaid-balance enforcement: a key owned by a
// portal user (Key.UserID != nil) whose balance has reached zero is
// rejected. Pass nil (the default) to keep billing off.
func (l *Limiter) SetBalanceGuard(g *BalanceGuard) { l.balance = g }

// New returns a Limiter. A nil spend cache disables the daily USD
// check (useful in tests, or when running without a DB-backed
// pricing pipeline); a nil rpm cache disables the RPM check. The
// model allow-list always runs.
func New(spend *SpendCache, rpm *RPMCache) *Limiter {
	return &Limiter{cache: spend, rpm: rpm}
}

// Check enforces both the model allow-list and the rolling 24h USD
// cap for k. Returns nil when the request may proceed.
//
// A nil k (auth disabled, dev mode) bypasses both checks — failure
// modes belong to the Auth Guard, not here.
//
// The daily-cap check is fail-open: a SpendCache / DB error logs a
// warning and allows the request. The alternative (fail-closed) would
// black-hole every request during a Postgres blip, which is a worse
// failure mode than a few minutes of unbilled usage.
func (l *Limiter) Check(ctx context.Context, k *apikey.Key, model string) *Reject {
	if k == nil {
		return nil
	}

	if !modelAllowed(model, k.AllowedModels) {
		return &Reject{
			Status: 403,
			Type:   "model_not_allowed",
			Message: fmt.Sprintf(
				"model %q is not in the allow-list for key %q",
				model, k.Name,
			),
		}
	}

	// RPM runs before the daily check because it's purely in-process
	// and rejects without touching the DB.
	if k.RPMLimit != nil && l.rpm != nil {
		if !l.rpm.Allow(k.Name, *k.RPMLimit) {
			return &Reject{
				Status: 429,
				Type:   "rate_limit_exceeded",
				Message: fmt.Sprintf(
					"key %q exceeded its rate limit of %d requests per minute",
					k.Name, *k.RPMLimit,
				),
			}
		}
	}

	// Prepaid balance gate (when billing is enabled). Runs for every key
	// with a portal owner, independent of any daily cap. Fail-open on a
	// source/DB error, same rationale as the daily cap below.
	if l.balance != nil && k.UserID != nil {
		bal, err := l.balance.Balance(ctx, *k.UserID)
		if err != nil {
			slog.Warn("balance check failed; request allowed",
				"key", k.Name, "err", err.Error())
		} else if bal <= 0 {
			return &Reject{
				Status: 402,
				Type:   "insufficient_balance",
				Message: fmt.Sprintf(
					"key %q has no remaining prepaid balance ($%.4f); please top up to continue",
					k.Name, bal),
			}
		}
	}

	if k.DailyUSDLimit == nil || l.cache == nil {
		return nil
	}
	spend, err := l.cache.Spend(ctx, k.Name)
	if err != nil {
		slog.Warn("daily limit check failed; request allowed",
			"key", k.Name, "err", err.Error())
		return nil
	}
	if spend >= *k.DailyUSDLimit {
		return &Reject{
			Status: 429,
			Type:   "daily_limit_exceeded",
			Message: fmt.Sprintf(
				"key %q reached its daily USD limit ($%.2f spent of $%.2f); the limit resets on a rolling 24h window",
				k.Name, spend, *k.DailyUSDLimit,
			),
		}
	}
	return nil
}

// RecordSpend folds the cost of a just-completed request into the per-key
// spend cache and, when billing is enabled and the key has a portal owner,
// debits that user's prepaid balance — so the next request sees
// up-to-date data without waiting for the WriteBuffer flush or a TTL
// refresh. Safe to call with a nil Limiter / key or a zero-cost request.
func (l *Limiter) RecordSpend(k *apikey.Key, usd float64) {
	if l == nil || k == nil || usd <= 0 {
		return
	}
	if l.cache != nil {
		l.cache.Add(k.Name, usd)
	}
	if l.balance != nil && k.UserID != nil {
		l.balance.Charge(*k.UserID, usd)
	}
}

// modelAllowed returns true when allow is empty (no restriction) or
// when model appears verbatim in allow. Match is case-sensitive on
// purpose — Anthropic's model identifiers are stable, lowercase
// strings; a case-insensitive match would hide typos.
func modelAllowed(model string, allow []string) bool {
	if len(allow) == 0 {
		return true
	}
	for _, m := range allow {
		if m == model {
			return true
		}
	}
	return false
}
