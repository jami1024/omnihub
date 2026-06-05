-- 0028_redemption_codes: prepaid gift / recharge codes.
--
-- An admin generates a batch of codes worth a fixed USD amount; a portal
-- user redeems one to credit their wallet (a 'redeem' wallet_ledger row).
-- Only the sha256 hash of the canonical code is stored — the cleartext
-- is shown exactly once at generation time, like an API key.

CREATE TABLE IF NOT EXISTS redemption_codes (
    id          BIGSERIAL     PRIMARY KEY,
    code_hash   CHAR(64)      NOT NULL UNIQUE,
    amount_usd  NUMERIC(14,6) NOT NULL CHECK (amount_usd > 0),
    status      VARCHAR(16)   NOT NULL DEFAULT 'unused',  -- unused | redeemed
    batch_id    VARCHAR(32),
    redeemed_by BIGINT        REFERENCES users(id) ON DELETE SET NULL,
    redeemed_at TIMESTAMPTZ,
    expires_at  TIMESTAMPTZ,
    created_by  VARCHAR(64),
    created_at  TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE redemption_codes IS
    'Prepaid gift codes. Only the code hash is stored; redeeming credits the wallet via a redeem ledger row.';

CREATE INDEX IF NOT EXISTS redemption_codes_batch_idx ON redemption_codes (batch_id);
