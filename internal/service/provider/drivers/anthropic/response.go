package anthropic

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/jami1024/omnihub/internal/ir"
)

// ParseResponse converts a non-streaming Anthropic /v1/messages
// response body into IR.
//
// The Anthropic response shape is a near-superset of UnifiedResponse, so
// the conversion is a direct json.Unmarshal. The caller (Forwarder) is
// responsible for status-code handling; ParseResponse assumes 2xx.
func (d *Driver) ParseResponse(resp *http.Response) (*ir.UnifiedResponse, error) {
	if resp == nil || resp.Body == nil {
		return nil, fmt.Errorf("anthropic: nil response body")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: read body: %w", err)
	}

	var out ir.UnifiedResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("anthropic: unmarshal response: %w", err)
	}
	return &out, nil
}
