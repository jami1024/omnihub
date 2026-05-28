package openai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/jami1024/omnihub/internal/ir"
	protoopenai "github.com/jami1024/omnihub/internal/protocol/openai"
	"github.com/jami1024/omnihub/internal/service/provider"
)

// BuildRequest translates an IR request into an OpenAI Chat Completions
// HTTP request, ready to be dispatched by the Forwarder.
func (d *Driver) BuildRequest(
	ctx context.Context,
	req *ir.UnifiedRequest,
	account *provider.Account,
) (*http.Request, error) {
	if req == nil {
		return nil, errors.New("openai: nil request")
	}
	if account == nil {
		return nil, errors.New("openai: nil account")
	}
	apiKey := account.Credential("api_key")
	if apiKey == "" {
		return nil, errors.New("openai: account missing api_key credential")
	}

	body, err := protoopenai.RequestToOpenAI(req)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal body: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, d.endpointURL(account), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai: build request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	if org := account.Credential("organization"); org != "" {
		httpReq.Header.Set("OpenAI-Organization", org)
	}
	if project := account.Credential("project"); project != "" {
		httpReq.Header.Set("OpenAI-Project", project)
	}

	// Forward SDK identifier headers the handler lifted from the inbound
	// request. Harmless for upstreams that ignore them.
	for k, v := range req.ClientMetadata {
		httpReq.Header.Set(k, v)
	}

	return httpReq, nil
}
