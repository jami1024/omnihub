package usage_test

import (
	"strings"
	"testing"

	"github.com/jami1024/omnihub/internal/service/usage"
)

func TestFromAnthropicJSON(t *testing.T) {
	body := []byte(`{
        "id": "msg_01ABC",
        "type": "message",
        "role": "assistant",
        "model": "claude-haiku-4-5-20251001",
        "content": [{"type":"text","text":"hi"}],
        "stop_reason": "end_turn",
        "usage": {
            "input_tokens": 12,
            "output_tokens": 4,
            "cache_creation_input_tokens": 0,
            "cache_read_input_tokens": 0
        }
    }`)

	got := usage.FromAnthropicJSON(body)
	if got.InputTokens != 12 || got.OutputTokens != 4 {
		t.Errorf("tokens: want 12/4, got %d/%d", got.InputTokens, got.OutputTokens)
	}
	if got.UpstreamRequestID != "msg_01ABC" {
		t.Errorf("UpstreamRequestID: want msg_01ABC, got %q", got.UpstreamRequestID)
	}
	if got.ActualModel != "claude-haiku-4-5-20251001" {
		t.Errorf("ActualModel: want full id, got %q", got.ActualModel)
	}
	if got.StopReason != "end_turn" {
		t.Errorf("StopReason: want end_turn, got %q", got.StopReason)
	}
}

func TestFromAnthropicJSONGarbage(t *testing.T) {
	got := usage.FromAnthropicJSON([]byte("not json at all"))
	if (got != usage.Usage{}) {
		t.Errorf("expected zero Usage on garbage input, got %+v", got)
	}
}

func TestSSESnifferMergesStartAndDelta(t *testing.T) {
	stream := strings.Join([]string{
		"event: message_start",
		`data: {"type":"message_start","message":{"id":"msg_1","model":"claude-haiku-4-5-20251001","usage":{"input_tokens":12,"output_tokens":3,"cache_creation_input_tokens":7,"cache_read_input_tokens":0}}}`,
		"",
		"event: content_block_start",
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}`,
		"",
		"event: message_delta",
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":19}}`,
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
		"",
	}, "\n")

	s := usage.NewSSESniffer()
	for _, line := range strings.Split(stream, "\n") {
		s.Feed([]byte(line + "\n"))
	}

	got := s.Result()
	if got.UpstreamRequestID != "msg_1" {
		t.Errorf("UpstreamRequestID: want msg_1, got %q", got.UpstreamRequestID)
	}
	if got.ActualModel != "claude-haiku-4-5-20251001" {
		t.Errorf("ActualModel: want full id, got %q", got.ActualModel)
	}
	if got.InputTokens != 12 {
		t.Errorf("InputTokens (from message_start): want 12, got %d", got.InputTokens)
	}
	if got.OutputTokens != 19 {
		t.Errorf("OutputTokens (from message_delta, overrides start): want 19, got %d", got.OutputTokens)
	}
	if got.CacheCreationInputTokens != 7 {
		t.Errorf("CacheCreationInputTokens (from message_start): want 7, got %d", got.CacheCreationInputTokens)
	}
	if got.StopReason != "end_turn" {
		t.Errorf("StopReason: want end_turn, got %q", got.StopReason)
	}
}

func TestSSESnifferIgnoresNonDataLines(t *testing.T) {
	s := usage.NewSSESniffer()
	for _, line := range []string{
		"event: message_start\n",
		":heartbeat\n",
		"\n",
		"id: 42\n",
		"retry: 1000\n",
	} {
		s.Feed([]byte(line))
	}
	if (s.Result() != usage.Usage{}) {
		t.Errorf("non-data lines should not produce usage, got %+v", s.Result())
	}
}

func TestSSESnifferIgnoresMalformedData(t *testing.T) {
	s := usage.NewSSESniffer()
	s.Feed([]byte("data: this is not json\n"))
	s.Feed([]byte(`data: {"unrelated":"thing"}` + "\n"))
	if (s.Result() != usage.Usage{}) {
		t.Errorf("malformed data should not produce usage, got %+v", s.Result())
	}
}
