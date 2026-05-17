// Package ir defines OmniHub's internal intermediate representation for
// LLM requests, responses, and stream chunks.
//
// All protocol adapters (Anthropic, OpenAI, Gemini, Bedrock) translate
// into and out of these types. Provider drivers and Guards operate
// exclusively on the IR; only the protocol adapter and driver layers
// know wire-format specifics.
//
// The IR is a superset of OpenAI / Anthropic / Bedrock features. The
// Extensions map on UnifiedRequest carries provider-specific fields the
// IR does not model directly, so a driver can round-trip non-standard
// fields without losing them.
package ir

import "encoding/json"

// Role identifies the speaker of a message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Extensions holds provider-specific fields the IR does not model
// directly. Values are passed through verbatim as raw JSON.
type Extensions map[string]json.RawMessage
