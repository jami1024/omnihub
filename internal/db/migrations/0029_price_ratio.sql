-- 0029_price_ratio: per-user billing markup.
--
-- A reseller charges users more than upstream cost. price_ratio is the
-- per-user multiplier applied to each request's cost to get the amount
-- billed to the user (sell price = cost_usd * price_ratio). Default 1.0
-- means "bill exactly cost" — so this migration changes nothing until an
-- operator sets a ratio. The per-request billed amount is recorded so the
-- balance is precise and not retroactively repriced when the ratio changes.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS price_ratio NUMERIC(8,4) NOT NULL DEFAULT 1.0
    CHECK (price_ratio >= 0);

-- billed_usd = cost_usd * the owning user's price_ratio at request time.
-- NULL for legacy rows / ownerless (admin) keys, where billed == cost.
ALTER TABLE message_requests
    ADD COLUMN IF NOT EXISTS billed_usd NUMERIC(14,6);
