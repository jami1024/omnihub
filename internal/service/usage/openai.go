package usage

import (
	"bytes"
	"encoding/json"
)

// openAITokens is the usage block shared by OpenAI's non-streaming
// response and its final streamed chunk.
type openAITokens struct {
	PromptTokens        int64 `json:"prompt_tokens"`
	CompletionTokens    int64 `json:"completion_tokens"`
	PromptTokensDetails *struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

// apply folds an OpenAI usage block into u. OpenAI's prompt_tokens
// includes cached tokens, so the cached portion is split out into
// CacheReadInputTokens to match the Anthropic-shaped accounting the rest
// of the gateway expects (InputTokens = fresh, CacheRead = reused).
func (t *openAITokens) apply(u *Usage) {
	cached := int64(0)
	if t.PromptTokensDetails != nil {
		cached = t.PromptTokensDetails.CachedTokens
	}
	u.InputTokens = t.PromptTokens - cached
	u.OutputTokens = t.CompletionTokens
	u.CacheReadInputTokens = cached
}

// FromOpenAIJSON parses a non-streaming OpenAI Chat Completions response
// body. Bodies that are not OpenAI-shaped JSON return a zero Usage.
func FromOpenAIJSON(body []byte) Usage {
	var resp struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage *openAITokens `json:"usage"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return Usage{}
	}
	u := Usage{UpstreamRequestID: resp.ID, ActualModel: resp.Model}
	if len(resp.Choices) > 0 {
		u.StopReason = resp.Choices[0].FinishReason
	}
	if resp.Usage != nil {
		resp.Usage.apply(&u)
	}
	return u
}

// OpenAISniffer extracts Usage from OpenAI SSE chunks as they stream by.
// Use one per request: Feed every line, then call Result.
//
// OpenAI only emits a usage block on the final chunk, and only when the
// request set stream_options.include_usage=true (the openai driver does
// this). The id/model are captured from the first chunk that carries
// them; finish_reason from the penultimate chunk.
type OpenAISniffer struct {
	u Usage
}

// NewOpenAISniffer returns a fresh OpenAI sniffer.
func NewOpenAISniffer() *OpenAISniffer { return &OpenAISniffer{} }

// Feed consumes one line of SSE wire bytes. Non-"data:" lines and the
// "[DONE]" sentinel are ignored.
func (s *OpenAISniffer) Feed(line []byte) {
	line = bytes.TrimRight(line, "\r\n")
	if !bytes.HasPrefix(line, []byte("data:")) {
		return
	}
	payload := bytes.TrimSpace(line[len("data:"):])
	if len(payload) == 0 || payload[0] != '{' {
		return
	}

	var ev struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage *openAITokens `json:"usage"`
	}
	if err := json.Unmarshal(payload, &ev); err != nil {
		return
	}
	if ev.ID != "" && s.u.UpstreamRequestID == "" {
		s.u.UpstreamRequestID = ev.ID
	}
	if ev.Model != "" && s.u.ActualModel == "" {
		s.u.ActualModel = ev.Model
	}
	for _, c := range ev.Choices {
		if c.FinishReason != "" {
			s.u.StopReason = c.FinishReason
		}
	}
	if ev.Usage != nil {
		ev.Usage.apply(&s.u)
	}
}

// Result returns the usage collected so far.
func (s *OpenAISniffer) Result() Usage { return s.u }
