-- 0039_account_allowed_models: per-account model allow-list.
--
--   accounts.allowed_models — JSONB array of model names this account is
--                             permitted to serve. An empty array (the
--                             default) means "no restriction — serve any
--                             model", preserving existing behaviour. When
--                             non-empty, the resolver skips this account
--                             for requests whose model is not listed, so a
--                             Codex/ChatGPT-subscription account can be
--                             pinned to the models its plan actually
--                             accepts instead of returning upstream 400s.
--
-- Mirrors the endpoints column shape (JSONB string array, NOT NULL,
-- defaulting to '[]'). The default makes this migration inert for
-- existing rows.

ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS allowed_models JSONB NOT NULL DEFAULT '[]'::jsonb;
