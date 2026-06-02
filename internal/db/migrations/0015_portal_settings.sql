-- 0015_portal_settings: admin-controlled policy for the end-user portal.
--
-- A single-row table (id is pinned to 1) holding the knobs an operator
-- sets from the admin console: whether open self-registration is allowed,
-- and the default / ceiling limits applied to keys that portal users
-- create for themselves. NULL means "no default" / "no cap".
--
-- These guard a live, openly-registerable portal whose keys spend real
-- money on the upstream, so a user can't mint an unlimited key.

CREATE TABLE IF NOT EXISTS portal_settings (
    id                    INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    signup_enabled        BOOLEAN NOT NULL DEFAULT TRUE,
    key_daily_usd_default DOUBLE PRECISION,
    key_daily_usd_max     DOUBLE PRECISION,
    key_rpm_max           INTEGER,
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO portal_settings (id) VALUES (1) ON CONFLICT (id) DO NOTHING;
