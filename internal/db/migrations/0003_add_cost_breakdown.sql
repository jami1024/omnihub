-- 0003_add_cost_breakdown: per-request cost breakdown for analytics.
--
-- cost_usd (added in 0002) stores the rolled-up total. cost_breakdown
-- adds the per-bucket detail so analytics can answer "where did the
-- spend go" — input vs output vs cache 5m/1h vs cache read.
--
-- JSONB shape (see pricing.CostBreakdown):
--   {
--     "input":            0.00003,
--     "output":           0.00002,
--     "cache_creation_5m": 0,
--     "cache_creation_1h": 0,
--     "cache_read":        0,
--     "total":             0.00005,
--     "multiplier":        1.0      -- present only when != 1.0
--   }
--
-- No index for now: JSONB lookups land on the breakdown rarely, the
-- rolled-up cost_usd column carries aggregate queries.

ALTER TABLE message_requests
    ADD COLUMN IF NOT EXISTS cost_breakdown JSONB;
