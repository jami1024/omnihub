-- 0033_announcements_plans: portal announcements and plan/pay-as-you-go billing.

CREATE TABLE IF NOT EXISTS announcements (
    id            BIGSERIAL PRIMARY KEY,
    title         VARCHAR(160) NOT NULL,
    body          TEXT NOT NULL,
    kind          VARCHAR(32) NOT NULL DEFAULT 'info'
                  CHECK (kind IN ('info', 'maintenance', 'pricing', 'model')),
    status        VARCHAR(32) NOT NULL DEFAULT 'draft'
                  CHECK (status IN ('draft', 'published', 'archived')),
    placement     VARCHAR(32) NOT NULL DEFAULT 'portal_home'
                  CHECK (placement IN ('portal_home', 'login', 'banner')),
    priority      INTEGER NOT NULL DEFAULT 0,
    starts_at     TIMESTAMPTZ,
    ends_at       TIMESTAMPTZ,
    dismissible   BOOLEAN NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS announcements_active_idx
    ON announcements (status, placement, priority DESC, created_at DESC);

CREATE TABLE IF NOT EXISTS plans (
    id                   BIGSERIAL PRIMARY KEY,
    name                 VARCHAR(120) NOT NULL,
    description          TEXT NOT NULL DEFAULT '',
    price_usd            NUMERIC(12, 6) NOT NULL DEFAULT 0 CHECK (price_usd >= 0),
    included_credit_usd  NUMERIC(14, 6) NOT NULL DEFAULT 0 CHECK (included_credit_usd >= 0),
    valid_days           INTEGER CHECK (valid_days IS NULL OR valid_days > 0),
    rpm_limit            INTEGER CHECK (rpm_limit IS NULL OR rpm_limit > 0),
    daily_usd_limit      NUMERIC(12, 6) CHECK (daily_usd_limit IS NULL OR daily_usd_limit >= 0),
    allowed_models       TEXT[] NOT NULL DEFAULT '{}',
    price_ratio          NUMERIC(8, 4) NOT NULL DEFAULT 1.0 CHECK (price_ratio >= 0),
    allow_payg_overage   BOOLEAN NOT NULL DEFAULT TRUE,
    enabled              BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order           INTEGER NOT NULL DEFAULT 0,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS plans_enabled_sort_idx
    ON plans (enabled, sort_order, id);

CREATE TABLE IF NOT EXISTS user_plan_grants (
    id                           BIGSERIAL PRIMARY KEY,
    user_id                      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    plan_id                      BIGINT REFERENCES plans(id) ON DELETE SET NULL,
    plan_name_snapshot           VARCHAR(120) NOT NULL,
    starts_at                    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at                   TIMESTAMPTZ,
    credit_granted_usd           NUMERIC(14, 6) NOT NULL DEFAULT 0 CHECK (credit_granted_usd >= 0),
    credit_remaining_usd         NUMERIC(14, 6) NOT NULL DEFAULT 0 CHECK (credit_remaining_usd >= 0),
    price_ratio_snapshot         NUMERIC(8, 4) NOT NULL DEFAULT 1.0 CHECK (price_ratio_snapshot >= 0),
    allow_payg_overage_snapshot  BOOLEAN NOT NULL DEFAULT TRUE,
    status                       VARCHAR(32) NOT NULL DEFAULT 'active'
                                 CHECK (status IN ('active', 'expired', 'depleted', 'revoked')),
    created_at                   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS user_plan_grants_active_idx
    ON user_plan_grants (user_id, status, starts_at DESC);

CREATE TABLE IF NOT EXISTS plan_usage_ledger (
    id                  BIGSERIAL PRIMARY KEY,
    user_plan_grant_id  BIGINT NOT NULL REFERENCES user_plan_grants(id) ON DELETE CASCADE,
    user_id             BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    amount_usd          NUMERIC(14, 6) NOT NULL CHECK (amount_usd > 0),
    request_created_at  TIMESTAMPTZ,
    note                TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS plan_usage_ledger_user_idx
    ON plan_usage_ledger (user_id, created_at DESC);
