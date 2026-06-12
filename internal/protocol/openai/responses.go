package openai

import (
	"encoding/json"
	"fmt"

	"github.com/jami1024/omnihub/internal/ir"
)

// ExtensionResponsesKey is the Extensions map key under which the FULL
// OpenAI Responses API request body is preserved. The Responses wire
// format (input items, instructions, reasoning, encrypted content) is
// deliberately NOT modelled in the IR: /v1/responses is a matched-pair
// pass-through surface, so the codex driver replays this blob with the
// minimal adjustments the upstream requires and nothing else.
const ExtensionResponsesKey = "openai_responses_payload"

// RequestFromResponses parses an OpenAI Responses API request just
// enough to route it: model and stream are lifted for the resolver /
// billing / metrics bookkeeping, a session-affinity hint is extracted
// for sticky binding, and the whole body is kept verbatim under
// Extensions[ExtensionResponsesKey].
//
// Returns (request, sessionAffinity, error). sessionAffinity is the
// client's own conversation identifier (prompt_cache_key — what Codex
// CLI sends — falling back to session_id / conversation_id), or "".
func RequestFromResponses(body []byte) (*ir.UnifiedRequest, string, error) {
	var probe struct {
		Model          string `json:"model"`
		Stream         bool   `json:"stream"`
		PromptCacheKey string `json:"prompt_cache_key"`
		SessionID      string `json:"session_id"`
		ConversationID string `json:"conversation_id"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return nil, "", fmt.Errorf("invalid JSON: %w", err)
	}
	if probe.Model == "" {
		return nil, "", fmt.Errorf("model is required")
	}

	affinity := probe.PromptCacheKey
	if affinity == "" {
		affinity = probe.SessionID
	}
	if affinity == "" {
		affinity = probe.ConversationID
	}

	raw := make(json.RawMessage, len(body))
	copy(raw, body)
	req := &ir.UnifiedRequest{
		Model:      probe.Model,
		Stream:     probe.Stream,
		Extensions: ir.Extensions{ExtensionResponsesKey: raw},
	}
	return req, affinity, nil
}
