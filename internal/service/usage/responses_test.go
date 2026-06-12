package usage

import "testing"

func TestFromResponsesJSON(t *testing.T) {
	body := []byte(`{
		"id": "resp_123",
		"model": "gpt-5-codex",
		"status": "completed",
		"usage": {
			"input_tokens": 1200,
			"output_tokens": 80,
			"input_tokens_details": {"cached_tokens": 1000},
			"output_tokens_details": {"reasoning_tokens": 30}
		}
	}`)
	u := FromResponsesJSON(body)
	if u.UpstreamRequestID != "resp_123" || u.ActualModel != "gpt-5-codex" || u.StopReason != "completed" {
		t.Fatalf("identifiers: %+v", u)
	}
	if u.InputTokens != 200 || u.CacheReadInputTokens != 1000 || u.OutputTokens != 80 {
		t.Fatalf("token split: %+v", u)
	}

	if got := FromResponsesJSON([]byte("not json")); got != (Usage{}) {
		t.Fatalf("garbage should yield zero usage: %+v", got)
	}
}

func TestResponsesSnifferStream(t *testing.T) {
	s := NewResponsesSniffer()
	lines := []string{
		`event: response.created`,
		`data: {"type":"response.created","response":{"id":"resp_9","model":"gpt-5-codex","status":"in_progress"}}`,
		``,
		`event: response.output_text.delta`,
		`data: {"type":"response.output_text.delta","delta":"hel"}`,
		``,
		`data: {"type":"response.output_text.delta","delta":"lo"}`,
		``,
		`event: response.completed`,
		`data: {"type":"response.completed","response":{"id":"resp_9","model":"gpt-5-codex","status":"completed","usage":{"input_tokens":50,"output_tokens":7,"input_tokens_details":{"cached_tokens":20}}}}`,
	}
	for _, l := range lines {
		s.Feed([]byte(l + "\n"))
	}
	u := s.Result()
	if u.UpstreamRequestID != "resp_9" || u.ActualModel != "gpt-5-codex" {
		t.Fatalf("identifiers: %+v", u)
	}
	if u.StopReason != "completed" {
		t.Fatalf("stop reason should end at completed, got %q", u.StopReason)
	}
	if u.InputTokens != 30 || u.CacheReadInputTokens != 20 || u.OutputTokens != 7 {
		t.Fatalf("tokens: %+v", u)
	}
}
