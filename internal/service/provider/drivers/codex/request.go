package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jami1024/omnihub/internal/ir"
	protoopenai "github.com/jami1024/omnihub/internal/protocol/openai"
	"github.com/jami1024/omnihub/internal/service/provider"
)

// BuildRequest replays the preserved Responses payload against the
// codex backend with the minimal adjustments the backend requires:
//
//   - model rewritten from req.Model (so account model-redirects work);
//   - store forced to false (subscription backend rejects stored runs);
//   - instructions always present (required field; "" when absent);
//   - temperature / top_p / max_output_tokens stripped (rejected by the
//     backend — the native CLI never sends them);
//   - stream mirrors the parsed req.Stream.
//
// Everything else in the client body passes through untouched.
func (d *Driver) BuildRequest(
	ctx context.Context,
	req *ir.UnifiedRequest,
	account *provider.Account,
) (*http.Request, error) {
	if req == nil {
		return nil, errors.New("codex: nil request")
	}
	if account == nil {
		return nil, errors.New("codex: nil account")
	}
	token := account.Credential("access_token")
	if token == "" {
		return nil, errors.New("codex: account has no access_token; import codex credentials first")
	}

	raw, ok := req.Extensions[protoopenai.ExtensionResponsesKey]
	if !ok {
		return nil, errors.New("codex: request lacks the Responses passthrough payload; this driver only serves /v1/responses")
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("codex: decode passthrough payload: %w", err)
	}

	// Copy before adjusting: the Extensions blob is shared across the
	// handler's failover retries, so the original must stay pristine.
	out := make(map[string]json.RawMessage, len(payload)+2)
	for k, v := range payload {
		out[k] = v
	}

	// Normalise the model to a backend-accepted slug (gpt-5 → gpt-5.4,
	// gpt-5-codex → gpt-5.3-codex, ...); unknown values pass through.
	model, err := json.Marshal(normalizeModel(req.Model))
	if err != nil {
		return nil, err
	}
	out["model"] = model
	out["store"] = json.RawMessage("false")
	// instructions must be a non-empty Codex CLI prompt or the backend
	// rejects the request. Keep the client's own when present; otherwise
	// inject the official base prompt.
	if !hasNonEmpty(out, "instructions") {
		ins, err := json.Marshal(defaultInstructions)
		if err != nil {
			return nil, err
		}
		out["instructions"] = ins
	}
	if req.Stream {
		out["stream"] = json.RawMessage("true")
	} else {
		delete(out, "stream")
	}
	// Strip generation/identity fields the codex backend rejects on
	// OAuth (subscription) traffic.
	for _, f := range unsupportedFields {
		delete(out, f)
	}

	body, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("codex: marshal body: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, d.endpointURL(account), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("codex: build request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if req.Stream {
		httpReq.Header.Set("Accept", "text/event-stream")
	} else {
		httpReq.Header.Set("Accept", "application/json")
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	if accountID := account.Credential("account_id"); accountID != "" {
		httpReq.Header.Set("chatgpt-account-id", accountID)
	}
	httpReq.Header.Set("OpenAI-Beta", "responses=experimental")
	httpReq.Header.Set("originator", originatorValue)
	// The codex backend validates the User-Agent looks like a Codex CLI;
	// the driver builds the request from scratch and does not forward the
	// downstream client's UA, so it must set one here.
	httpReq.Header.Set("User-Agent", userAgent)

	return httpReq, nil
}

// unsupportedFields are body fields the codex (subscription) backend
// rejects; they are stripped before dispatch. Mirrors sub2api's list.
var unsupportedFields = []string{
	"temperature",
	"top_p",
	"max_output_tokens",
	"max_completion_tokens",
	"frequency_penalty",
	"presence_penalty",
	"user",
	"metadata",
	"prompt_cache_retention",
	"safety_identifier",
	"stream_options",
}

// hasNonEmpty reports whether the payload carries a non-empty value for
// the given key (a present-but-"" instructions still counts as missing).
func hasNonEmpty(payload map[string]json.RawMessage, key string) bool {
	v, ok := payload[key]
	if !ok {
		return false
	}
	s := strings.TrimSpace(string(v))
	return s != "" && s != `""` && s != "null"
}
