-- 0002_add_cost_usd: per-request USD cost.
--
-- NUMERIC(12, 6) handles up to $999,999.999999 with 1e-6 precision —
-- comfortably more than any single LLM call has ever cost while
-- leaving room for analytics rollups across millions of rows. NULL is
-- used when the model is not in the pricing table (operator should
-- notice the warning log and update internal/service/pricing).

ALTER TABLE message_requests
    ADD COLUMN IF NOT EXISTS cost_usd NUMERIC(12, 6);

CREATE INDEX IF NOT EXISTS message_requests_cost_usd_idx
    ON message_requests (cost_usd)
    WHERE cost_usd IS NOT NULL;
