-- 0036_account_auth_model: upstream account authentication model.
--
-- Phase 1 of the upstream-OAuth plugin design (docs/architecture/
-- upstream-oauth-plugins.md). Adds the columns that let an account
-- authenticate upstream by something other than a static API key —
-- OAuth / imported CLI credentials / service account — without
-- changing any existing behaviour.
--
-- Two groups of columns:
--
--   admin-configured  — auth_type, auth_plugin, client_profile,
--                        client_profile_config. Set when the account
--                        is created/edited.
--   runtime-maintained — auth_status, auth_subject, auth_email,
--                        auth_plan, auth_expires_at, last_refresh_at,
--                        refresh_error. Written by the TokenManager /
--                        auth plugin (phase 2+), never by the admin form.
--
-- Defaults make this migration inert: every existing row becomes
-- auth_type='api_key', auth_status='ok', client_profile_config='{}',
-- which is exactly the current api-key behaviour.

ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS auth_type TEXT NOT NULL DEFAULT 'api_key'
        CHECK (auth_type IN ('api_key', 'oauth', 'imported_oauth', 'service_account', 'adc', 'worker')),
    ADD COLUMN IF NOT EXISTS auth_plugin TEXT,
    ADD COLUMN IF NOT EXISTS auth_status TEXT NOT NULL DEFAULT 'ok'
        CHECK (auth_status IN (
            'ok', 'expiring', 'refreshing', 'refresh_failed', 'login_required',
            'revoked', 'quota_exhausted', 'rate_limited', 'tier_insufficient',
            'unsupported_region', 'disabled'
        )),
    ADD COLUMN IF NOT EXISTS auth_subject TEXT,
    ADD COLUMN IF NOT EXISTS auth_email TEXT,
    ADD COLUMN IF NOT EXISTS auth_plan TEXT,
    ADD COLUMN IF NOT EXISTS auth_expires_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_refresh_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS refresh_error TEXT,
    ADD COLUMN IF NOT EXISTS client_profile TEXT,
    ADD COLUMN IF NOT EXISTS client_profile_config JSONB NOT NULL DEFAULT '{}'::jsonb;
