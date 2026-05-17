// Package session derives a deterministic session key from a request
// and maintains a short-lived sticky binding from session key to
// upstream account.
//
// Why stickiness matters: Anthropic prompt cache is scoped per account.
// Routing the same conversation through the same upstream maximises
// cache hits, and a cache hit costs roughly 10% of an input-token
// charge — a 10x cost saving on the cached prefix. Without
// stickiness, weighted-random selection scatters consecutive turns
// across accounts and invalidates the cache.
//
// The key is derived from the virtual API key, the requested model,
// the system prompt prefix, and the first user message text. These
// components are stable across the turns of one conversation but
// differ between separate conversations, so the binding survives
// follow-up turns while still partitioning unrelated traffic.
package session

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/jami1024/omnihub/internal/ir"
)

// systemTextLimit caps how many bytes of the system prompt are
// folded into the key. Long system prompts often contain bulky
// reference material that may drift between calls (timestamps,
// dynamic injections); hashing only the prefix keeps the binding
// stable in the common case.
const systemTextLimit = 8192

// KeyFor derives the session key for a request. virtualKey scopes the
// binding to one caller (so two clients with identical prompts do
// not share an upstream). Returns "" when the request lacks any
// content the key can be based on — the caller treats that as "no
// stickiness for this request".
func KeyFor(virtualKey string, req *ir.UnifiedRequest) string {
	if req == nil {
		return ""
	}

	h := sha256.New()
	if virtualKey != "" {
		h.Write([]byte(virtualKey))
	}
	h.Write([]byte{0})
	h.Write([]byte(req.Model))
	h.Write([]byte{0})

	systemBytes := writeSystemPrefix(h, req.System)
	h.Write([]byte{0})
	userBytes := writeFirstUserText(h, req.Messages)

	if systemBytes == 0 && userBytes == 0 {
		// Nothing distinctive to hash beyond virtualKey/model: do not
		// bind so requests like an empty health-check do not pin the
		// caller to one upstream.
		return ""
	}

	digest := h.Sum(nil)
	return hex.EncodeToString(digest[:16]) // 128-bit slice is plenty
}

// writeSystemPrefix folds up to systemTextLimit bytes of system-prompt
// text into h. Anthropic accepts the system prompt as an array of
// content blocks; only `text` blocks are hashed.
func writeSystemPrefix(h interface{ Write([]byte) (int, error) }, blocks []ir.ContentBlock) int {
	written := 0
	for _, b := range blocks {
		if b.Type != ir.BlockText {
			continue
		}
		text := b.Text
		if written+len(text) > systemTextLimit {
			text = text[:systemTextLimit-written]
		}
		n, _ := h.Write([]byte(text))
		written += n
		if written >= systemTextLimit {
			break
		}
	}
	return written
}

// writeFirstUserText folds the text of the FIRST user message into h.
// Multi-turn conversations re-send the original first user message,
// so this remains stable across the turns of one chat.
func writeFirstUserText(h interface{ Write([]byte) (int, error) }, messages []ir.Message) int {
	written := 0
	for _, m := range messages {
		if m.Role != ir.RoleUser {
			continue
		}
		for _, b := range m.Content {
			if b.Type != ir.BlockText {
				continue
			}
			n, _ := h.Write([]byte(b.Text))
			written += n
		}
		break
	}
	return written
}
