-- 0007_request_client_metadata: per-request client identity.
--
-- Captures the immediate caller's IP and User-Agent for every
-- gateway request. Useful for audit, abuse investigation, and
-- per-client cost attribution beyond just the virtual key label.
--
-- client_ip is VARCHAR(45) to comfortably hold an IPv6 string
-- (max 39 chars for plain v6, +4 for zone id). user_agent is TEXT
-- because real-world UA strings can exceed 512 chars (Claude Code
-- adds rich metadata; corporate VPN agents are notorious).

ALTER TABLE message_requests
    ADD COLUMN IF NOT EXISTS client_ip   VARCHAR(45),
    ADD COLUMN IF NOT EXISTS user_agent  TEXT;

-- Optional index for IP-based queries (e.g. "show me activity from
-- this IP over the last hour"). Cheap to maintain on append-only
-- writes; comment out if you do not need it.
CREATE INDEX IF NOT EXISTS message_requests_client_ip_idx
    ON message_requests (client_ip, created_at DESC)
    WHERE client_ip IS NOT NULL;
