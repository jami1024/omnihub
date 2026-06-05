-- 0026_alert_channels: operator alert delivery channels with hot reload.
--
-- Rows drive the in-memory alert notifier set. The url column holds a
-- webhook endpoint (often with an embedded token) and is encrypted at
-- rest by the repository, exactly like account credentials. A statement
-- trigger broadcasts changes so the gateway's channel pool refreshes
-- within milliseconds without a restart.

CREATE TABLE IF NOT EXISTS alert_channels (
    id          BIGSERIAL PRIMARY KEY,
    type        VARCHAR(32)  NOT NULL,          -- webhook | feishu | dingtalk
    name        VARCHAR(128) NOT NULL,
    url         TEXT         NOT NULL,          -- encrypted at rest
    enabled     BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    created_by  VARCHAR(64)
);

COMMENT ON TABLE alert_channels IS
    'Operator alert delivery channels (webhook/feishu/dingtalk). url is encrypted at rest.';

CREATE OR REPLACE FUNCTION omnihub_notify_alert_channels_changed()
RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('omnihub_alert_channels_changed', TG_OP);
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS omnihub_alert_channels_notify ON alert_channels;
CREATE TRIGGER omnihub_alert_channels_notify
    AFTER INSERT OR UPDATE OR DELETE ON alert_channels
    FOR EACH STATEMENT
    EXECUTE FUNCTION omnihub_notify_alert_channels_changed();
