-- 0030_signup_bonus: optional starting credit for new portal users.
--
-- When signup_bonus_usd > 0, a newly registered user is granted that
-- amount as a wallet credit (a 'bonus' wallet_ledger row) so they can use
-- their keys immediately even with prepaid billing on. 0 disables it.

ALTER TABLE portal_settings
    ADD COLUMN IF NOT EXISTS signup_bonus_usd NUMERIC(14,6) NOT NULL DEFAULT 0
    CHECK (signup_bonus_usd >= 0);
