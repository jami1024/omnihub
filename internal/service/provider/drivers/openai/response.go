package openai

import (
	"fmt"
	"io"
	"net/http"

	"github.com/jami1024/omnihub/internal/ir"
	protoopenai "github.com/jami1024/omnihub/internal/protocol/openai"
)

// ParseResponse converts a non-streaming OpenAI Chat Completions response
// body into IR. The caller (Forwarder) handles status codes and owns the
// body lifecycle; ParseResponse assumes 2xx.
func (d *Driver) ParseResponse(resp *http.Response) (*ir.UnifiedResponse, error) {
	if resp == nil || resp.Body == nil {
		return nil, fmt.Errorf("openai: nil response body")
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai: read body: %w", err)
	}
	return protoopenai.ResponseToIR(body)
}
