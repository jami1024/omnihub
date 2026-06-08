-- 0034_request_billing_split: store how much of a request was paid by plan credit vs wallet.

ALTER TABLE message_requests
    ADD COLUMN IF NOT EXISTS plan_billed_usd NUMERIC(14,6),
    ADD COLUMN IF NOT EXISTS wallet_billed_usd NUMERIC(14,6),
    ADD COLUMN IF NOT EXISTS plan_grant_id BIGINT REFERENCES user_plan_grants(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS message_requests_plan_grant_idx
    ON message_requests (plan_grant_id, created_at DESC)
    WHERE plan_grant_id IS NOT NULL;
