package usage

import (
	"bytes"
	"encoding/json"
)

// Responses parses OpenAI Responses API bodies (the /v1/responses
// surface, served by the openai-codex driver).
var Responses Parser = responsesParser{}

type responsesParser struct{}

func (responsesParser) FromJSON(body []byte) Usage { return FromResponsesJSON(body) }
func (responsesParser) NewSniffer() Sniffer        { return NewResponsesSniffer() }

// responsesTokens is the usage block on a Responses API response
// object. input_tokens INCLUDES the cached portion (same convention as
// Chat Completions' prompt_tokens), so the cached share is split out.
type responsesTokens struct {
	InputTokens        int64 `json:"input_tokens"`
	OutputTokens       int64 `json:"output_tokens"`
	InputTokensDetails *struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
}

func (t *responsesTokens) apply(u *Usage) {
	cached := int64(0)
	if t.InputTokensDetails != nil {
		cached = t.InputTokensDetails.CachedTokens
	}
	u.InputTokens = t.InputTokens - cached
	u.OutputTokens = t.OutputTokens
	u.CacheReadInputTokens = cached
}

// responsesObject is the subset of a Responses API response object the
// gateway accounts for. Status doubles as the stop reason ("completed",
// "incomplete", "failed").
type responsesObject struct {
	ID     string           `json:"id"`
	Model  string           `json:"model"`
	Status string           `json:"status"`
	Usage  *responsesTokens `json:"usage"`
}

func (r *responsesObject) apply(u *Usage) {
	if r.ID != "" && u.UpstreamRequestID == "" {
		u.UpstreamRequestID = r.ID
	}
	if r.Model != "" && u.ActualModel == "" {
		u.ActualModel = r.Model
	}
	if r.Status != "" {
		u.StopReason = r.Status
	}
	if r.Usage != nil {
		r.Usage.apply(u)
	}
}

// FromResponsesJSON parses a non-streaming Responses API body. Bodies
// that are not Responses-shaped JSON return a zero Usage.
func FromResponsesJSON(body []byte) Usage {
	var resp responsesObject
	if err := json.Unmarshal(body, &resp); err != nil {
		return Usage{}
	}
	var u Usage
	resp.apply(&u)
	return u
}

// ResponsesSniffer extracts Usage from Responses API SSE events. Use
// one per request: Feed every line, then call Result.
//
// The stream carries typed events: response.created announces id/model
// up front; only response.completed (and the failed/incomplete
// variants) carries the usage block, nested under "response".
type ResponsesSniffer struct {
	u Usage
}

// NewResponsesSniffer returns a fresh Responses sniffer.
func NewResponsesSniffer() *ResponsesSniffer { return &ResponsesSniffer{} }

// Feed consumes one line of SSE wire bytes. Non-"data:" lines are
// ignored (the codex backend also emits "event:" lines; the type field
// inside the data payload is authoritative).
func (s *ResponsesSniffer) Feed(line []byte) {
	line = bytes.TrimRight(line, "\r\n")
	if !bytes.HasPrefix(line, []byte("data:")) {
		return
	}
	payload := bytes.TrimSpace(line[len("data:"):])
	if len(payload) == 0 || payload[0] != '{' {
		return
	}

	var ev struct {
		Type     string           `json:"type"`
		Response *responsesObject `json:"response"`
	}
	if err := json.Unmarshal(payload, &ev); err != nil {
		return
	}
	switch ev.Type {
	case "response.created", "response.in_progress",
		"response.completed", "response.failed", "response.incomplete":
		if ev.Response != nil {
			ev.Response.apply(&s.u)
		}
	}
}

// Result returns the usage collected so far.
func (s *ResponsesSniffer) Result() Usage { return s.u }
