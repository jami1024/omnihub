-- 0035_api_key_billing_mode: per-key billing source.
--
-- A key is billed in one of two ways, decided per key rather than per user:
--
--   payg  — bills the owner's WALLET only, at users.price_ratio. Never
--           touches plan credit.
--   plan  — bills the owner's active plan grant FIRST (at the grant's
--           price_ratio_snapshot), then wallet overage only if the grant
--           allows it; with no active grant the request is rejected.
--
-- Default 'payg' so every existing key keeps its current wallet-only
-- behaviour — this migration changes nothing until a key is set to 'plan'
-- AND billing is enabled. Ownerless/admin keys ignore the column entirely.

ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS billing_mode TEXT NOT NULL DEFAULT 'payg'
    CHECK (billing_mode IN ('payg', 'plan'));
