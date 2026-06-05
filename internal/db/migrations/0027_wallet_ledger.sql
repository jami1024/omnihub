-- 0027_wallet_ledger: prepaid credit ledger (append-only).
--
-- A user's balance = SUM(wallet_ledger.amount_usd) − lifetime request
-- cost (message_requests.cost_usd over the user's keys). Only the
-- credit side lives here: top-ups, redemptions, refunds, and manual
-- adjustments (which may be negative). Consumption is NOT duplicated
-- here — it is already recorded per request in message_requests, so the
-- ledger stays small and balance is derived, never double-counted.

CREATE TABLE IF NOT EXISTS wallet_ledger (
    id          BIGSERIAL     PRIMARY KEY,
    user_id     BIGINT        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind        VARCHAR(16)   NOT NULL,   -- topup | redeem | refund | adjust
    amount_usd  NUMERIC(14,6) NOT NULL,   -- positive credit; adjust may be negative
    note        TEXT,
    created_by  VARCHAR(64),
    created_at  TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE wallet_ledger IS
    'Append-only prepaid credit events. Balance = SUM(amount_usd) minus lifetime request cost.';

CREATE INDEX IF NOT EXISTS wallet_ledger_user_idx ON wallet_ledger (user_id, created_at DESC);
