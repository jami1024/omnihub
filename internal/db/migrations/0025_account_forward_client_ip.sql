-- 0025_account_forward_client_ip: per-account opt-in to forward the
-- real client IP to the upstream.
--
-- When TRUE, the forwarder sets X-Forwarded-For to the gateway-resolved
-- client IP (the same value already stored in message_requests.client_ip,
-- resolved against OMNIHUB_TRUSTED_PROXIES). All OTHER forwarding headers
-- (X-Real-IP, X-Forwarded-Host/Proto/Port, Forwarded, CF-Connecting-IP,
-- True-Client-IP) remain stripped regardless of this flag, and client
-- auth is never forwarded. Mirrors claude-code-hub's keepClientIp.
-- Default FALSE preserves the existing strip-everything behaviour.

ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS forward_client_ip BOOLEAN NOT NULL DEFAULT FALSE;
