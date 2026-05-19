package ir

// RectifyThinkingBlocks returns a deep-copied UnifiedRequest with
// extended-thinking artifacts stripped:
//
//   - Top-level Thinking config is cleared (server-side thinking off
//     for this attempt).
//   - Every `thinking` content block is downgraded to a plain `text`
//     block, preserving the reasoning text so the conversation still
//     makes sense.
//   - Every `redacted_thinking` block is dropped (opaque blob; no
//     plaintext fallback is possible).
//
// The intent is to recover from Anthropic's "Invalid `signature` in
// `thinking` block" 400 without losing the user-visible conversation:
// callers run this once after such an error, retry the same upstream
// with the rectified request, and serve whatever the retry returns.
// The original request is left untouched.
func RectifyThinkingBlocks(req *UnifiedRequest) *UnifiedRequest {
	if req == nil {
		return nil
	}
	out := *req
	out.Thinking = nil
	out.Messages = make([]Message, len(req.Messages))
	for i, msg := range req.Messages {
		out.Messages[i] = Message{
			Role:    msg.Role,
			Content: rectifyContent(msg.Content),
		}
	}
	return &out
}

func rectifyContent(blocks []ContentBlock) []ContentBlock {
	out := make([]ContentBlock, 0, len(blocks))
	for _, b := range blocks {
		switch b.Type {
		case BlockThinking:
			// Preserve the text content as a plain text block so the
			// conversation history still reads naturally. An empty
			// thinking block contributes nothing and is dropped.
			if b.Thinking != "" {
				out = append(out, TextBlock(b.Thinking))
			}
		case BlockRedactedThinking:
			// Opaque payload — nothing meaningful to keep.
			continue
		default:
			out = append(out, b)
		}
	}
	return out
}
