-- 0006_accounts_notify_trigger: instant in-memory pool refresh on
-- accounts table mutations.
--
-- The gateway listens on 'omnihub_accounts_changed' via pgx and
-- triggers Pool.Refresh whenever a notification arrives. The
-- 30-second periodic refresh inside the pool is kept as a safety net
-- — a missed notification (process restart, connection drop) is
-- still corrected within one refresh tick.
--
-- FOR EACH STATEMENT (not FOR EACH ROW) keeps bulk operations from
-- producing notification storms; the listener only needs to know
-- "something changed, re-read the table".

CREATE OR REPLACE FUNCTION omnihub_notify_accounts_changed()
RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('omnihub_accounts_changed', TG_OP);
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS omnihub_accounts_notify ON accounts;
CREATE TRIGGER omnihub_accounts_notify
    AFTER INSERT OR UPDATE OR DELETE ON accounts
    FOR EACH STATEMENT
    EXECUTE FUNCTION omnihub_notify_accounts_changed();
