package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

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

	model, err := json.Marshal(req.Model)
	if err != nil {
		return nil, err
	}
	out["model"] = model
	out["store"] = json.RawMessage("false")
	if _, ok := out["instructions"]; !ok {
		out["instructions"] = json.RawMessage(`""`)
	}
	if req.Stream {
		out["stream"] = json.RawMessage("true")
	} else {
		delete(out, "stream")
	}
	delete(out, "temperature")
	delete(out, "top_p")
	delete(out, "max_output_tokens")

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

	return httpReq, nil
}
