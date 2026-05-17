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
}

// Credential returns the value for the given credential key, or "" if
// the account or the key is missing.
func (a *Account) Credential(key string) string {
	if a == nil {
		return ""
	}
	return a.Credentials[key]
}
