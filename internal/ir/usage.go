package ir

// Usage reports token accounting for a single LLM call. All fields are
// optional; drivers fill what the upstream reports.
type Usage struct {
	// InputTokens is the prompt token count.
	InputTokens int64 `json:"input_tokens,omitempty"`

	// OutputTokens is the generation token count.
	OutputTokens int64 `json:"output_tokens,omitempty"`

	// CacheCreationInputTokens is the count of prompt tokens written
	// to the Anthropic prompt cache on this call.
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens,omitempty"`

	// CacheReadInputTokens is the count of prompt tokens served
	// from the Anthropic prompt cache on this call.
	CacheReadInputTokens int64 `json:"cache_read_input_tokens,omitempty"`
}

// Add accumulates other into u. Used by the streaming aggregator to
// fold per-chunk usage deltas into a running total.
func (u *Usage) Add(other Usage) {
	u.InputTokens += other.InputTokens
	u.OutputTokens += other.OutputTokens
	u.CacheCreationInputTokens += other.CacheCreationInputTokens
	u.CacheReadInputTokens += other.CacheReadInputTokens
}

// TotalTokens returns the sum of input and output tokens. Cache-related
// counters are excluded because they are subsets of InputTokens.
func (u Usage) TotalTokens() int64 {
	return u.InputTokens + u.OutputTokens
}
