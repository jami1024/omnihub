-- 0011_blocked_ips: per-IP policies (hard block + soft limits) with
-- hot reload.
--
-- One row per IP under policy. The semantics are:
--
--   * all three limit columns NULL  →  hard block (return 403 at front door)
--   * any limit column non-NULL     →  soft cap (allow until the cap is hit,
--                                     then 429 with Retry-After-ish guidance)
--
-- Exact-IP match only — CIDR support is intentionally deferred until
-- there is a concrete need. The pool fits comfortably in memory and
-- the middleware does an O(1) map lookup.
--
-- The table is hot-reloaded via LISTEN/NOTIFY so operators can add or
-- remove policy with a single INSERT/UPDATE/DELETE and see the effect
-- within a network round-trip.

CREATE TABLE IF NOT EXISTS blocked_ips (
    ip                VARCHAR(45) PRIMARY KEY,                    -- IPv4 or IPv6 literal
    reason            TEXT,                                       -- free-form note for ops
    rpm_limit         INTEGER         CHECK (rpm_limit IS NULL OR rpm_limit > 0),
    tpm_limit         BIGINT          CHECK (tpm_limit IS NULL OR tpm_limit > 0),
    concurrent_limit  INTEGER         CHECK (concurrent_limit IS NULL OR concurrent_limit > 0),
    created_at        TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    created_by        VARCHAR(64)
);

COMMENT ON TABLE  blocked_ips IS
    'IP policies. NULL limit columns = hard block; non-NULL = soft cap.';
COMMENT ON COLUMN blocked_ips.rpm_limit IS
    'Per-IP requests per minute (token bucket). NULL = unlimited.';
COMMENT ON COLUMN blocked_ips.tpm_limit IS
    'Per-IP fresh input tokens per minute (input + cache_creation). NULL = unlimited.';
COMMENT ON COLUMN blocked_ips.concurrent_limit IS
    'Per-IP simultaneous in-flight requests. NULL = unlimited.';

CREATE OR REPLACE FUNCTION omnihub_notify_blocked_ips_changed()
RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('omnihub_blocked_ips_changed', TG_OP);
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS omnihub_blocked_ips_notify ON blocked_ips;
CREATE TRIGGER omnihub_blocked_ips_notify
    AFTER INSERT OR UPDATE OR DELETE ON blocked_ips
    FOR EACH STATEMENT
    EXECUTE FUNCTION omnihub_notify_blocked_ips_changed();
