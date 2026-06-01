-- 0013_model_prices: model pricing as a first-class DB entity.
--
-- OmniHub previously priced requests from a hard-coded Go table
-- (pricing.Default()). This table makes prices data: an operator can
-- sync them from an upstream source (LiteLLM's
-- model_prices_and_context_window.json) and override individual models
-- by hand via the admin UI. The in-memory price pool overlays these
-- rows on top of the built-in defaults and refreshes within
-- milliseconds via the NOTIFY trigger below.
--
-- The `source` column distinguishes where a row came from so a sync
-- pass never clobbers a manual override:
--   'litellm' — written by the LiteLLM sync; overwritten on re-sync.
--   'manual'  — created/edited in the admin UI; sync leaves it alone.
--
-- Field names mirror LiteLLM / pricing.Price (USD per token, not per
-- million). Costs are DOUBLE PRECISION — per-token rates are tiny
-- floats (e.g. 2.5e-6) and exact decimal accounting happens downstream
-- in message_requests.cost_usd (NUMERIC).

CREATE TABLE IF NOT EXISTS model_prices (
    id                                        BIGSERIAL    PRIMARY KEY,
    model                                     VARCHAR(128) NOT NULL UNIQUE,
    input_cost_per_token                      DOUBLE PRECISION NOT NULL DEFAULT 0,
    output_cost_per_token                     DOUBLE PRECISION NOT NULL DEFAULT 0,
    cache_creation_input_token_cost           DOUBLE PRECISION NOT NULL DEFAULT 0,
    cache_creation_input_token_cost_above_1hr DOUBLE PRECISION NOT NULL DEFAULT 0,
    cache_read_input_token_cost               DOUBLE PRECISION NOT NULL DEFAULT 0,
    source                                    VARCHAR(16)  NOT NULL DEFAULT 'manual',
    created_at                                TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at                                TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS model_prices_source_idx ON model_prices (source);

-- Instant refresh trigger — same pattern as accounts (0006) and
-- api_keys (0008). The price pool LISTENs on this channel and rebuilds
-- its overlay on any insert/update/delete.
CREATE OR REPLACE FUNCTION omnihub_notify_model_prices_changed()
RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('omnihub_model_prices_changed', TG_OP);
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS omnihub_model_prices_notify ON model_prices;
CREATE TRIGGER omnihub_model_prices_notify
    AFTER INSERT OR UPDATE OR DELETE ON model_prices
    FOR EACH STATEMENT
    EXECUTE FUNCTION omnihub_notify_model_prices_changed();
