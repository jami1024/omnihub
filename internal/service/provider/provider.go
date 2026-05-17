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
}

// Credential returns the value for the given credential key, or "" if
// the account or the key is missing.
func (a *Account) Credential(key string) string {
	if a == nil {
		return ""
	}
	return a.Credentials[key]
}
