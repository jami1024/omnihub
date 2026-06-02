-- 0022_account_proxy: per-account outbound proxy.
--
-- proxy_url routes this account's upstream requests through an HTTP /
-- HTTPS / SOCKS5 proxy (e.g. to reach a region-locked provider or to
-- pin a stable egress IP), mirroring claude-code-hub's per-provider
-- proxy. Empty / NULL means "connect directly". The forwarder caches one
-- HTTP client per distinct proxy URL so this adds no per-request cost.
--
-- Stored verbatim (may include credentials in the userinfo), so protect
-- the DB the same way credentials are protected.

ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS proxy_url VARCHAR(255);
