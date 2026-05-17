-- 0001_init: minimal schema for end-to-end request logging.
--
-- One row per inbound /v1/messages call. Inserted at the start of the
-- request, then patched in place as the request progresses (auth →
-- forward → usage extracted → response complete).
--
-- Token columns default to 0 so callers can rely on NOT NULL semantics;
-- they get patched with real values once the response is parsed.

CREATE TABLE IF NOT EXISTS message_requests (
    id                              BIGSERIAL    PRIMARY KEY,
    created_at                      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    -- Request metadata
    key_name                        VARCHAR(64),
    method                          VARCHAR(8)   NOT NULL,
    path                            VARCHAR(128) NOT NULL,
    model                           VARCHAR(128) NOT NULL,
    actual_model                    VARCHAR(128),
    stream                          BOOLEAN      NOT NULL DEFAULT FALSE,

    -- Outcome
    status_code                     INTEGER,
    duration_ms                     BIGINT,
    ttfb_ms                         BIGINT,
    error_message                   TEXT,

    -- Token usage
    input_tokens                    BIGINT       NOT NULL DEFAULT 0,
    output_tokens                   BIGINT       NOT NULL DEFAULT 0,
    cache_creation_input_tokens     BIGINT       NOT NULL DEFAULT 0,
    cache_read_input_tokens         BIGINT       NOT NULL DEFAULT 0,

    -- Provider
    provider_name                   VARCHAR(64)  NOT NULL,
    account_name                    VARCHAR(64)  NOT NULL,
    upstream_request_id             VARCHAR(64)
);

CREATE INDEX IF NOT EXISTS message_requests_created_at_idx
    ON message_requests (created_at DESC);

CREATE INDEX IF NOT EXISTS message_requests_key_name_idx
    ON message_requests (key_name, created_at DESC);

CREATE INDEX IF NOT EXISTS message_requests_model_idx
    ON message_requests (model, created_at DESC);
