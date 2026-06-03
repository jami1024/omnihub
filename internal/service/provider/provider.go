// Package provider defines the Driver contract that all upstream LLM
// integrations implement, and a Registry that wires drivers and
// resolved upstream accounts together at runtime.
//
// A "driver" is pure transformation logic: it owns the rules for
// translating an [ir.UnifiedRequest] into a provider-specific HTTP
// request and decoding the corresponding response back into IR. Drivers
// do not own the HTTP client, retry logic, or circuit breaker — those
// live in the Forwarder Guard so they can be tuned and shared across
// all drivers.
//
// An "account" is one set of upstream credentials. Many accounts may
// share a driver (e.g. 50 Anthropic accounts all use the anthropic
// driver), and the Resolver Guard picks which account to dispatch a
// given request through.
package provider

import "time"

// Account is one set of upstream credentials plus the configuration
// the driver needs to route a request. Accounts are persisted in the
// database and loaded into memory by the account service.
type Account struct {
	// ID is the row id from the accounts table.
	ID int64

	// Name is a human-readable label.
	Name string

	// Provider identifies the driver (e.g. "anthropic", "openai",
	// "bedrock"). It must match a Driver registered in the Registry.
	Provider string

	// BaseURL optionally overrides the driver's default upstream URL
	// (used for self-hosted or proxy endpoints). Empty means the
	// driver uses its built-in default.
	BaseURL string

	// Credentials carries provider-specific authentication material.
	// Drivers read keys from this map by well-known names, e.g.
	// "api_key", "aws_access_key_id", "aws_region".
	Credentials map[string]string

	// CostMultiplier scales the upstream cost stored against this
	// account. Values <= 0 or == 1.0 mean "no scaling" (the upstream
	// price is recorded as-is). Reseller deployments use multipliers
	// > 1.0 to mark up cost; teams with internal subsidies use < 1.0.
	//
	// The multiplier is applied at the handler boundary, after the
	// pricing table returns the base breakdown. The applied factor is
	// preserved in the persisted breakdown so analytics can recover
	// the base cost.
	CostMultiplier float64

	// Weight controls weighted-random selection within a priority
	// bucket. Higher weight = higher relative pick probability.
	// Zero is treated as 1 (so a freshly inserted row participates
	// minimally) by the resolver.
	Weight int

	// Priority groups accounts into "tiers". The resolver picks from
	// the LOWEST-numbered priority bucket that has at least one
	// healthy account, falling through to higher numbers as accounts
	// in the top bucket exhaust their quotas. 0 is the default tier.
	Priority int

	// CircuitFailureThreshold / CircuitOpenDuration /
	// CircuitHalfOpenSuccess optionally override the gateway-wide
	// circuit-breaker defaults for this account. A nil value falls
	// back to the OMNIHUB_CIRCUIT_* env-driven default. The pointer
	// shape lets the DB distinguish "use default" (NULL) from
	// "disable for this account" (explicit 0 / minimum).
	CircuitFailureThreshold *int
	CircuitOpenDuration     *time.Duration
	CircuitHalfOpenSuccess  *int

	// ModelRedirects rewrites the requested model name before the
	// driver builds its upstream request. Rules are evaluated in order;
	// the first match wins. Empty means "forward the model unchanged".
	ModelRedirects []ModelRedirect

	// DailyUSDLimit / TotalUSDLimit are optional per-account spend
	// ceilings the resolver enforces (rolling 24h and lifetime,
	// respectively). Nil means "no cap".
	DailyUSDLimit *float64
	TotalUSDLimit *float64

	// GroupID / GroupName / GroupCostMultiplier describe the optional
	// provider group this account belongs to. GroupID nil means
	// ungrouped. GroupCostMultiplier defaults to 1.0 (loaded via JOIN);
	// it stacks on top of the account's own CostMultiplier for billing.
	GroupID             *int64
	GroupName           string
	GroupCostMultiplier float64

	// CustomHeaders are extra outbound HTTP headers applied to every
	// upstream request for this account. Empty means none. They cannot
	// override the gateway's security / streaming invariants (forwarded-
	// for stripping, identity encoding) — those are re-asserted after.
	CustomHeaders map[string]string

	// Endpoints are ADDITIONAL upstream base URLs tried (in order) after
	// BaseURL when a request fails with a transport error or retriable
	// status. They share this account's credentials. Empty means
	// "BaseURL only".
	Endpoints []string

	// HealthProbeEnabled opts this account into (or out of) the active
	// background health prober. Nil means "inherit the global default"
	// (OMNIHUB_HEALTH_PROBE_ENABLED); a non-nil value is an explicit
	// per-account override. Mirrors the nullable circuit overrides.
	HealthProbeEnabled *bool

	// ProxyURL optionally routes this account's upstream requests through
	// an http/https/socks5 proxy. Empty means connect directly.
	ProxyURL string

	// ParamOverrides force per-account generation parameters (max_tokens,
	// temperature, top_p, thinking budget) onto each request. The zero
	// value is a no-op.
	ParamOverrides ParamOverrides

	// ActiveWindows restrict when the account is routable. Empty means
	// "always active". ActiveTimezone is the IANA name the windows are
	// evaluated in (empty = UTC).
	ActiveWindows  []ActiveWindow
	ActiveTimezone string

	// ForwardClientIP, when true, forwards the resolved real client IP to
	// the upstream as a fresh X-Forwarded-For header (some upstreams want
	// it for risk-control / billing). Default false keeps the safe
	// strip-everything behaviour; all OTHER forwarding headers are
	// stripped regardless, and client auth is never forwarded.
	ForwardClientIP bool
}

// EndpointURLs returns the ordered list of base URLs to try for this
// account: BaseURL first, then any extra Endpoints, de-duplicated while
// preserving order. The result always has at least one element (BaseURL,
// which may be "" → the driver's default endpoint).
func (a *Account) EndpointURLs() []string {
	if a == nil {
		return []string{""}
	}
	seen := map[string]struct{}{a.BaseURL: {}}
	out := []string{a.BaseURL}
	for _, e := range a.Endpoints {
		if _, dup := seen[e]; dup {
			continue
		}
		seen[e] = struct{}{}
		out = append(out, e)
	}
	return out
}

// EffectiveCostMultiplier is the factor applied to upstream cost for
// billing: the account's own multiplier times its group's (when
// grouped). A zero/unset group multiplier is treated as 1.0 so an
// ungrouped account bills at exactly its own multiplier.
func (a *Account) EffectiveCostMultiplier() float64 {
	if a == nil {
		return 1
	}
	g := a.GroupCostMultiplier
	if g <= 0 {
		g = 1
	}
	return a.CostMultiplier * g
}

// Credential returns the value for the given credential key, or "" if
// the account or the key is missing.
func (a *Account) Credential(key string) string {
	if a == nil {
		return ""
	}
	return a.Credentials[key]
}
