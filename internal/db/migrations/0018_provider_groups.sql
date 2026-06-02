-- 0018_provider_groups: optional grouping of upstream accounts.
--
-- A provider group is an organizational bucket with a shared cost
-- multiplier (claude-code-hub's provider groups, reimplemented). An
-- account may belong to at most one group; the group's multiplier
-- stacks on top of the account's own (effective = account × group), so
-- an operator can mark up / subsidise a whole set of accounts at once
-- without editing each row.
--
-- group_id is nullable (ungrouped is the default) and ON DELETE SET
-- NULL so removing a group quietly un-groups its accounts rather than
-- deleting them.

CREATE TABLE IF NOT EXISTS provider_groups (
    id              BIGSERIAL    PRIMARY KEY,
    name            VARCHAR(64)  NOT NULL UNIQUE,
    cost_multiplier NUMERIC(8,4) NOT NULL DEFAULT 1.0  CHECK (cost_multiplier >= 0),
    description     TEXT         NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS group_id BIGINT
        REFERENCES provider_groups (id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS accounts_group_id_idx
    ON accounts (group_id)
    WHERE group_id IS NOT NULL;

-- A group's cost_multiplier feeds the account pool (loaded via JOIN), so
-- editing a group must trigger the same in-memory refresh as an account
-- change. Reuse the existing accounts-changed channel the gateway
-- already listens on.
CREATE OR REPLACE FUNCTION omnihub_notify_groups_changed()
RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('omnihub_accounts_changed', 'GROUP_' || TG_OP);
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS omnihub_provider_groups_notify ON provider_groups;
CREATE TRIGGER omnihub_provider_groups_notify
    AFTER INSERT OR UPDATE OR DELETE ON provider_groups
    FOR EACH STATEMENT
    EXECUTE FUNCTION omnihub_notify_groups_changed();
