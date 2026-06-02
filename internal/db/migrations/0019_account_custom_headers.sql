-- 0019_account_custom_headers: per-account custom outbound headers.
--
-- custom_headers is a JSONB object of header-name → value pairs that the
-- forwarder applies to every upstream request for this account (org ids,
-- routing hints, beta flags, etc.), mirroring claude-code-hub's custom
-- header support. They are applied right after the driver builds the
-- request but BEFORE the gateway strips client-forwarding headers and
-- forces identity encoding, so an operator header can never override
-- those security / streaming invariants.
--
-- Default '{}' means "no custom headers". JSONB keeps the set atomic
-- with the account row and reloads for free over the NOTIFY path.

ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS custom_headers JSONB NOT NULL DEFAULT '{}'::jsonb;
