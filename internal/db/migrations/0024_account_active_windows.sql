-- 0024_account_active_windows: per-account active time windows.
--
-- active_windows restricts WHEN an account is routable — the resolver
-- skips it outside its windows (claude-code-hub's active time windows,
-- reimplemented). Empty '[]' means "always active". Each window is
--   {"days": [1,2,3,4,5], "start": "09:00", "end": "18:00"}
-- where days are 0=Sunday..6=Saturday (empty = every day) and start/end
-- are "HH:MM" local times in active_timezone. A window whose start is
-- after its end wraps past midnight.
--
-- active_timezone is the IANA name the windows are evaluated in
-- (NULL/empty = UTC).

ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS active_windows  JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS active_timezone VARCHAR(64);
