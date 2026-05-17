package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jami1024/omnihub/internal/ir"
	"github.com/jami1024/omnihub/internal/service/provider"
)

// BuildRequest translates an IR request into a fully signed Anthropic
// HTTP request, ready to be dispatched by the Forwarder.
func (d *Driver) BuildRequest(
	ctx context.Context,
	req *ir.UnifiedRequest,
	account *provider.Account,
) (*http.Request, error) {
	if req == nil {
		return nil, errors.New("anthropic: nil request")
	}
	if account == nil {
		return nil, errors.New("anthropic: nil account")
	}
	apiKey := account.Credential("api_key")
	if apiKey == "" {
		return nil, errors.New("anthropic: account missing api_key credential")
	}

	body, err := json.Marshal(ToWireBody(req))
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal body: %w", err)
	}

	url := strings.TrimRight(d.baseURL(account), "/") + messagesPath
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("anthropic: build request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion(req))

	if beta := strings.Join(req.AnthropicBeta, ","); beta != "" {
		httpReq.Header.Set("anthropic-beta", beta)
	}

	return httpReq, nil
}

// anthropicVersion returns the API version to send in the
// anthropic-version header.
func anthropicVersion(req *ir.UnifiedRequest) string {
	if req != nil && req.AnthropicVersion != "" {
		// Bedrock-style versions (e.g. "bedrock-2023-05-31") should not
		// reach this driver; if they do, fall back to the direct API
		// default rather than pass them through.
		if !strings.HasPrefix(req.AnthropicVersion, "bedrock-") {
			return req.AnthropicVersion
		}
	}
	return DefaultAnthropicVersion
}

// MessagesBody is the on-the-wire payload sent to /v1/messages.
//
// It mirrors UnifiedRequest minus fields that travel as HTTP headers
// (anthropic_version, anthropic_beta) or that are IR-internal
// (Extensions, Bedrock-only fields).
//
// Exported so sibling drivers (e.g. claude_platform) that share the
// Anthropic wire format can reuse it without copying the schema.
type MessagesBody struct {
	Model         string             `json:"model"`
	Messages      []ir.Message       `json:"messages"`
	System        []ir.ContentBlock  `json:"system,omitempty"`
	Tools         []ir.Tool          `json:"tools,omitempty"`
	ToolChoice    *ir.ToolChoice     `json:"tool_choice,omitempty"`
	Stream        bool               `json:"stream,omitempty"`
	MaxTokens     int                `json:"max_tokens"`
	Temperature   *float64           `json:"temperature,omitempty"`
	TopP          *float64           `json:"top_p,omitempty"`
	TopK          *int               `json:"top_k,omitempty"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
	Thinking      *ir.ThinkingConfig `json:"thinking,omitempty"`
	Metadata      map[string]any     `json:"metadata,omitempty"`
}

// ToWireBody projects an IR request into the Anthropic /v1/messages
// wire body. Exported for reuse by sibling drivers.
func ToWireBody(req *ir.UnifiedRequest) *MessagesBody {
	return &MessagesBody{
		Model:         req.Model,
		Messages:      req.Messages,
		System:        req.System,
		Tools:         req.Tools,
		ToolChoice:    req.ToolChoice,
		Stream:        req.Stream,
		MaxTokens:     req.MaxTokens,
		Temperature:   req.Temperature,
		TopP:          req.TopP,
		TopK:          req.TopK,
		StopSequences: req.StopSequences,
		Thinking:      req.Thinking,
		Metadata:      req.Metadata,
	}
}
