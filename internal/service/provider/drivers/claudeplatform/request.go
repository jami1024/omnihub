package claudeplatform

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
	"github.com/jami1024/omnihub/internal/service/provider/drivers/anthropic"
)

// BuildRequest translates an IR request into a fully-formed HTTP
// request for the Claude Platform on AWS endpoint.
//
// Required account credentials:
//
//   - api_key       — AWS Marketplace-issued Anthropic API key
//   - aws_region    — AWS region (e.g. "us-east-1")
//   - workspace_id  — Anthropic workspace ID for the AWS account
//
// The account's BaseURL, if non-empty, overrides the regional
// endpoint construction (useful for VPC endpoints / proxies).
func (d *Driver) BuildRequest(
	ctx context.Context,
	req *ir.UnifiedRequest,
	account *provider.Account,
) (*http.Request, error) {
	if req == nil {
		return nil, errors.New("claude-platform: nil request")
	}
	if account == nil {
		return nil, errors.New("claude-platform: nil account")
	}

	apiKey := account.Credential("api_key")
	if apiKey == "" {
		// SigV4 mode is not implemented in this driver yet. Surface a
		// clear error rather than silently dispatching unsigned.
		return nil, errors.New("claude-platform: missing api_key credential (SigV4 auth not yet supported)")
	}
	workspaceID := workspaceIDFromAccount(account)
	if workspaceID == "" {
		return nil, errors.New("claude-platform: missing workspace_id credential")
	}

	baseURL, err := endpointURL(account)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(anthropic.ToWireBody(req))
	if err != nil {
		return nil, fmt.Errorf("claude-platform: marshal body: %w", err)
	}

	url := strings.TrimRight(baseURL, "/") + messagesPath
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("claude-platform: build request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion(req))
	httpReq.Header.Set("anthropic-workspace-id", workspaceID)

	if beta := strings.Join(req.AnthropicBeta, ","); beta != "" {
		httpReq.Header.Set("anthropic-beta", beta)
	}

	// Forward SDK identifier headers the handler lifted from the
	// inbound request. The AWS Marketplace endpoint accepts the
	// same x-stainless-* / x-app set as direct Anthropic.
	for k, v := range req.ClientMetadata {
		httpReq.Header.Set(k, v)
	}

	return httpReq, nil
}

// endpointURL returns the regional base URL, honouring an explicit
// account.BaseURL override.
func endpointURL(account *provider.Account) (string, error) {
	if account.BaseURL != "" {
		return account.BaseURL, nil
	}
	region := account.Credential("aws_region")
	if region == "" {
		return "", errors.New("claude-platform: missing aws_region credential")
	}
	return fmt.Sprintf(urlTemplate, region), nil
}

// workspaceIDFromAccount looks up the workspace identifier under any
// of the conventional credential keys.
func workspaceIDFromAccount(account *provider.Account) string {
	for _, key := range []string{"workspace_id", "aws_workspace_id", "anthropic_workspace_id"} {
		if v := account.Credential(key); v != "" {
			return v
		}
	}
	return ""
}

// anthropicVersion returns the API version to send in the
// anthropic-version header. Bedrock-style versions (e.g.
// "bedrock-2023-05-31") are ignored — they belong to a different
// endpoint family.
func anthropicVersion(req *ir.UnifiedRequest) string {
	if req != nil && req.AnthropicVersion != "" && !strings.HasPrefix(req.AnthropicVersion, "bedrock-") {
		return req.AnthropicVersion
	}
	return anthropic.DefaultAnthropicVersion
}
