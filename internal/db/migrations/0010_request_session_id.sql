-- 0010_request_session_id: per-conversation correlation.
--
-- Claude Code attaches an x-claude-code-session-id header to every
-- request from the same CLI session. Storing it makes "real users"
-- counting tractable when many clients share one NAT or IP — the
-- (IP, UA) heuristic is a noisy lower bound; session_id is the
-- ground truth.
--
-- VARCHAR(64) comfortably holds the UUID format Claude Code emits
-- (36 chars) plus headroom for any future ID scheme.

ALTER TABLE message_requests
    ADD COLUMN IF NOT EXISTS session_id VARCHAR(64);

-- Cardinality is high (one per CLI session), so a partial index
-- restricted to non-null rows keeps the index hot path lean.
CREATE INDEX IF NOT EXISTS message_requests_session_id_idx
    ON message_requests (session_id, created_at DESC)
    WHERE session_id IS NOT NULL;
