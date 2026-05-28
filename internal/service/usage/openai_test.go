package usage_test

import (
	"strings"
	"testing"

	"github.com/jami1024/omnihub/internal/service/usage"
)

func TestFromOpenAIJSON(t *testing.T) {
	body := []byte(`{
		"id": "chatcmpl-1",
		"model": "gpt-4o-2024",
		"choices": [{"index": 0, "finish_reason": "stop"}],
		"usage": {"prompt_tokens": 30, "completion_tokens": 12, "total_tokens": 42, "prompt_tokens_details": {"cached_tokens": 10}}
	}`)
	got := usage.FromOpenAIJSON(body)
	// prompt(30) includes cached(10) -> input 20, cache_read 10
	if got.InputTokens != 20 || got.OutputTokens != 12 || got.CacheReadInputTokens != 10 {
		t.Errorf("tokens = %+v", got)
	}
	if got.UpstreamRequestID != "chatcmpl-1" || got.ActualModel != "gpt-4o-2024" {
		t.Errorf("id/model = %q/%q", got.UpstreamRequestID, got.ActualModel)
	}
	if got.StopReason != "stop" {
		t.Errorf("stop = %q, want stop", got.StopReason)
	}
}

func TestFromOpenAIJSONGarbage(t *testing.T) {
	if got := usage.FromOpenAIJSON([]byte("not json")); got != (usage.Usage{}) {
		t.Errorf("garbage should yield zero usage, got %+v", got)
	}
}

func TestOpenAISniffer(t *testing.T) {
	lines := []string{
		`data: {"id":"c1","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
		`data: {"id":"c1","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"hi"}}]}`,
		`data: {"id":"c1","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`data: {"id":"c1","model":"gpt-4o","choices":[],"usage":{"prompt_tokens":18,"completion_tokens":7,"prompt_tokens_details":{"cached_tokens":3}}}`,
		"data: [DONE]",
	}
	s := usage.NewOpenAISniffer()
	for _, l := range lines {
		s.Feed([]byte(l + "\n"))
	}
	got := s.Result()
	if got.InputTokens != 15 || got.OutputTokens != 7 || got.CacheReadInputTokens != 3 {
		t.Errorf("tokens = %+v", got)
	}
	if got.UpstreamRequestID != "c1" || got.ActualModel != "gpt-4o" {
		t.Errorf("id/model = %q/%q", got.UpstreamRequestID, got.ActualModel)
	}
	if got.StopReason != "stop" {
		t.Errorf("stop = %q", got.StopReason)
	}
}

func TestParsersSatisfyInterface(t *testing.T) {
	// Compile-time-ish check that both parsers produce working sniffers.
	for name, p := range map[string]usage.Parser{"anthropic": usage.Anthropic, "openai": usage.OpenAI} {
		sn := p.NewSniffer()
		sn.Feed([]byte("data: {}\n"))
		_ = sn.Result()
		_ = p.FromJSON([]byte("{}"))
		if strings.TrimSpace(name) == "" {
			t.Fatal("unreachable")
		}
	}
}
