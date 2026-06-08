# Announcements and Billing Plans Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add portal announcements plus two billing modes: plan package credits and pay-as-you-go wallet billing, with plan credits consumed before wallet balance.

**Architecture:** Persist announcements, plan templates, user plan grants, and plan consumption ledger in Postgres. Keep repositories focused by domain, expose admin CRUD APIs and portal read APIs, then integrate billing guards so active plan credit is checked and charged before wallet balance. Frontend follows the existing React Query + page component pattern, with admin management pages and portal display pages.

**Tech Stack:** Go, Gin, pgx, Postgres migrations, React, TypeScript, TanStack Query, Vite, existing OmniHub auth and layout components.

---

## File Structure

### Backend migrations

- Create `internal/db/migrations/0033_announcements_plans.sql`
  - Creates `announcements`, `plans`, `user_plan_grants`, `plan_usage_ledger`.

### Backend repositories

- Create `internal/repository/announcement.go`
  - Announcement CRUD and active portal announcement lookup.
- Create `internal/repository/plan.go`
  - Plan template CRUD, portal plan listing, user grant creation/listing, atomic plan credit consumption.
- Modify `internal/repository/message_request.go`
  - Add optional plan/wallet split fields if persisted on request rows.
- Modify `internal/repository/usage_stats.go`
  - Return plan/wallet split in portal request log when available.

### Backend services

- Create `internal/service/billing/billing.go`
  - Determines effective user billing source: active plan first, wallet fallback second.
- Modify `internal/service/limits/balance.go`
  - Either extend BalanceGuard or add a sibling guard for plan+wallet available balance.

### Backend handlers

- Create `internal/handler/admin/announcements.go`
- Create `internal/handler/admin/plans.go`
- Create `internal/handler/portal/announcements.go`
- Create `internal/handler/portal/plans.go`
- Modify `internal/handler/admin/users.go`
  - Include active plan summary or expose user plan grant endpoint.
- Modify `cmd/omnihub/main.go`
  - Wire repositories and routes.
- Modify gateway handlers only where billing precheck/charge must be integrated.

### Frontend data layer

- Create `web/src/lib/announcements.ts`
- Create `web/src/lib/plans.ts`
- Modify `web/src/lib/portalData.ts`
  - Add portal announcements, plans, and current plan types/hooks.
- Modify `web/src/lib/users.ts`
  - Add plan grant API hooks if user page assigns plans.

### Frontend pages

- Create `web/src/pages/Announcements.tsx`
- Create `web/src/pages/Plans.tsx`
- Create `web/src/pages/portal/PortalPlans.tsx`
- Modify `web/src/pages/portal/PortalOverview.tsx`
  - Show announcements and current plan summary.
- Modify `web/src/pages/portal/PortalWallet.tsx`
  - Explain wallet is pay-as-you-go and overage fallback.
- Modify `web/src/pages/Users.tsx`
  - Add assign/revoke plan controls or link to user plan grants.
- Modify `web/src/components/SettingsLayout.tsx` and `web/src/components/PortalLayout.tsx`
  - Add navigation entries.
- Modify `web/src/lib/locales/zh.ts` and `web/src/lib/locales/en.ts`.

---

## Task 1: Database Schema

**Files:**
- Create: `internal/db/migrations/0033_announcements_plans.sql`

- [ ] **Step 1: Write migration with announcements and plans**

Create `internal/db/migrations/0033_announcements_plans.sql` with this schema:

```sql
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
```

- [ ] **Step 2: Verify migration syntax indirectly**

Run:

```bash
GOCACHE=$PWD/.codex-gocache go test ./internal/db ./internal/repository
```

Expected: packages compile. The migration itself is applied during Docker startup later.

- [ ] **Step 3: Commit**

```bash
git add internal/db/migrations/0033_announcements_plans.sql
git commit -m "feat: add announcement and plan schema"
```

---

## Task 2: Announcement Repository and Tests

**Files:**
- Create: `internal/repository/announcement.go`
- Create: `internal/repository/announcement_test.go`

- [ ] **Step 1: Write repository tests**

Create `internal/repository/announcement_test.go` using a fake pgx pool is hard in this project, so keep unit tests around validation helpers and use handler tests for behavior. Add this test for status/kind normalization in repository-level helper functions:

```go
package repository

import "testing"

func TestValidateAnnouncementRejectsUnknownKind(t *testing.T) {
	row := Announcement{Title: "T", Body: "B", Kind: "sale", Status: "draft", Placement: "portal_home"}
	if err := ValidateAnnouncement(row); err == nil {
		t.Fatal("expected unknown kind to be rejected")
	}
}

func TestValidateAnnouncementAcceptsPublishedBanner(t *testing.T) {
	row := Announcement{Title: "T", Body: "B", Kind: "maintenance", Status: "published", Placement: "banner"}
	if err := ValidateAnnouncement(row); err != nil {
		t.Fatalf("expected valid announcement, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
GOCACHE=$PWD/.codex-gocache go test ./internal/repository -run TestValidateAnnouncement
```

Expected: FAIL because `Announcement` and `ValidateAnnouncement` do not exist.

- [ ] **Step 3: Implement repository**

Create `internal/repository/announcement.go`:

```go
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrAnnouncementNotFound = errors.New("announcement not found")

type Announcement struct {
	ID          int64      `json:"id"`
	Title       string     `json:"title"`
	Body        string     `json:"body"`
	Kind        string     `json:"kind"`
	Status      string     `json:"status"`
	Placement   string     `json:"placement"`
	Priority    int        `json:"priority"`
	StartsAt    *time.Time `json:"starts_at"`
	EndsAt      *time.Time `json:"ends_at"`
	Dismissible bool       `json:"dismissible"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type AnnouncementRepo struct{ pool *pgxpool.Pool }

func NewAnnouncementRepo(pool *pgxpool.Pool) *AnnouncementRepo { return &AnnouncementRepo{pool: pool} }

func ValidateAnnouncement(a Announcement) error {
	if a.Title == "" { return fmt.Errorf("title is required") }
	if a.Body == "" { return fmt.Errorf("body is required") }
	if !oneOf(a.Kind, "info", "maintenance", "pricing", "model") { return fmt.Errorf("invalid kind") }
	if !oneOf(a.Status, "draft", "published", "archived") { return fmt.Errorf("invalid status") }
	if !oneOf(a.Placement, "portal_home", "login", "banner") { return fmt.Errorf("invalid placement") }
	return nil
}

func oneOf(v string, allowed ...string) bool {
	for _, a := range allowed { if v == a { return true } }
	return false
}

func (r *AnnouncementRepo) List(ctx context.Context) ([]Announcement, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,title,body,kind,status,placement,priority,starts_at,ends_at,dismissible,created_at,updated_at FROM announcements ORDER BY priority DESC, created_at DESC`)
	if err != nil { return nil, fmt.Errorf("list announcements: %w", err) }
	defer rows.Close()
	return scanAnnouncements(rows)
}

func (r *AnnouncementRepo) ListActive(ctx context.Context, placement string, now time.Time) ([]Announcement, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,title,body,kind,status,placement,priority,starts_at,ends_at,dismissible,created_at,updated_at FROM announcements WHERE status='published' AND placement=$1 AND (starts_at IS NULL OR starts_at <= $2) AND (ends_at IS NULL OR ends_at > $2) ORDER BY priority DESC, created_at DESC`, placement, now)
	if err != nil { return nil, fmt.Errorf("list active announcements: %w", err) }
	defer rows.Close()
	return scanAnnouncements(rows)
}

func (r *AnnouncementRepo) Create(ctx context.Context, a Announcement) (int64, error) {
	if err := ValidateAnnouncement(a); err != nil { return 0, err }
	var id int64
	err := r.pool.QueryRow(ctx, `INSERT INTO announcements (title,body,kind,status,placement,priority,starts_at,ends_at,dismissible) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`, a.Title,a.Body,a.Kind,a.Status,a.Placement,a.Priority,a.StartsAt,a.EndsAt,a.Dismissible).Scan(&id)
	if err != nil { return 0, fmt.Errorf("create announcement: %w", err) }
	return id, nil
}

func (r *AnnouncementRepo) Update(ctx context.Context, id int64, a Announcement) error {
	if err := ValidateAnnouncement(a); err != nil { return err }
	ct, err := r.pool.Exec(ctx, `UPDATE announcements SET title=$2,body=$3,kind=$4,status=$5,placement=$6,priority=$7,starts_at=$8,ends_at=$9,dismissible=$10,updated_at=NOW() WHERE id=$1`, id,a.Title,a.Body,a.Kind,a.Status,a.Placement,a.Priority,a.StartsAt,a.EndsAt,a.Dismissible)
	if err != nil { return fmt.Errorf("update announcement: %w", err) }
	if ct.RowsAffected() == 0 { return ErrAnnouncementNotFound }
	return nil
}

func (r *AnnouncementRepo) Delete(ctx context.Context, id int64) error {
	ct, err := r.pool.Exec(ctx, `DELETE FROM announcements WHERE id=$1`, id)
	if err != nil { return fmt.Errorf("delete announcement: %w", err) }
	if ct.RowsAffected() == 0 { return ErrAnnouncementNotFound }
	return nil
}

func scanAnnouncements(rows pgx.Rows) ([]Announcement, error) {
	out := []Announcement{}
	for rows.Next() {
		var a Announcement
		if err := rows.Scan(&a.ID,&a.Title,&a.Body,&a.Kind,&a.Status,&a.Placement,&a.Priority,&a.StartsAt,&a.EndsAt,&a.Dismissible,&a.CreatedAt,&a.UpdatedAt); err != nil { return nil, err }
		out = append(out, a)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run tests**

```bash
GOCACHE=$PWD/.codex-gocache go test ./internal/repository -run TestValidateAnnouncement
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/repository/announcement.go internal/repository/announcement_test.go
git commit -m "feat: add announcement repository"
```

---

## Task 3: Announcement Admin and Portal APIs

**Files:**
- Create: `internal/handler/admin/announcements.go`
- Create: `internal/handler/admin/announcements_test.go`
- Create: `internal/handler/portal/announcements.go`
- Create: `internal/handler/portal/announcements_test.go`
- Modify: `cmd/omnihub/main.go`

- [ ] **Step 1: Write admin handler tests**

Create `internal/handler/admin/announcements_test.go` with fake store implementing `List`, `Create`, `Update`, `Delete`. Test:

```go
func TestCreateAnnouncementRejectsEmptyTitle(t *testing.T) { /* POST body with empty title returns 400 */ }
func TestListAnnouncementsReturnsRows(t *testing.T) { /* GET returns {announcements:[...]} */ }
func TestUpdateAnnouncementNotFound(t *testing.T) { /* repository.ErrAnnouncementNotFound maps to 404 */ }
```

- [ ] **Step 2: Write portal handler test**

Create `internal/handler/portal/announcements_test.go`:

```go
func TestPortalAnnouncementsReturnsOnlyStoreRows(t *testing.T) {
	// fake store returns one published portal_home announcement.
	// GET /announcements?placement=portal_home returns that row.
}
```

- [ ] **Step 3: Run tests to verify they fail**

```bash
GOCACHE=$PWD/.codex-gocache go test ./internal/handler/admin ./internal/handler/portal -run Announcement
```

Expected: FAIL because handlers do not exist.

- [ ] **Step 4: Implement handlers**

Admin endpoints:

```go
GET    /admin/api/announcements
POST   /admin/api/announcements
PATCH  /admin/api/announcements/:id
DELETE /admin/api/announcements/:id
```

Portal endpoint:

```go
GET /portal/api/announcements?placement=portal_home
```

Validation rules:

- title and body required.
- kind in `info|maintenance|pricing|model`.
- status in `draft|published|archived`.
- placement in `portal_home|login|banner`.
- priority any integer.

- [ ] **Step 5: Wire routes in `cmd/omnihub/main.go`**

Inside `mountAdminRoutes`, after settings repositories:

```go
announcementRepo := repository.NewAnnouncementRepo(pool)
authed.GET("/announcements", adminhandler.ListAnnouncementsHandler(announcementRepo))
authed.POST("/announcements", adminhandler.CreateAnnouncementHandler(announcementRepo))
authed.PATCH("/announcements/:id", adminhandler.UpdateAnnouncementHandler(announcementRepo))
authed.DELETE("/announcements/:id", adminhandler.DeleteAnnouncementHandler(announcementRepo))
puser.GET("/announcements", portalhandler.AnnouncementsHandler(announcementRepo))
```

- [ ] **Step 6: Run tests**

```bash
GOCACHE=$PWD/.codex-gocache go test ./internal/handler/admin ./internal/handler/portal
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/handler/admin/announcements.go internal/handler/admin/announcements_test.go internal/handler/portal/announcements.go internal/handler/portal/announcements_test.go cmd/omnihub/main.go
git commit -m "feat: add announcement APIs"
```

---

## Task 4: Plan Repository and Tests

**Files:**
- Create: `internal/repository/plan.go`
- Create: `internal/repository/plan_test.go`

- [ ] **Step 1: Write validation tests**

Create `internal/repository/plan_test.go`:

```go
package repository

import "testing"

func TestValidatePlanRejectsNegativeCredit(t *testing.T) {
	p := Plan{Name: "Starter", IncludedCreditUSD: -1, PriceRatio: 1, Enabled: true}
	if err := ValidatePlan(p); err == nil { t.Fatal("expected negative credit rejected") }
}

func TestValidatePlanAcceptsPaygOveragePlan(t *testing.T) {
	p := Plan{Name: "Starter", Description: "Basic", PriceUSD: 9, IncludedCreditUSD: 10, ValidDays: intPtr(30), PriceRatio: 1, AllowPaygOverage: true, Enabled: true}
	if err := ValidatePlan(p); err != nil { t.Fatalf("valid plan rejected: %v", err) }
}

func intPtr(v int) *int { return &v }
```

- [ ] **Step 2: Run test to verify it fails**

```bash
GOCACHE=$PWD/.codex-gocache go test ./internal/repository -run TestValidatePlan
```

Expected: FAIL because `Plan` and `ValidatePlan` do not exist.

- [ ] **Step 3: Implement `internal/repository/plan.go`**

Define:

```go
type Plan struct {
	ID int64 `json:"id"`
	Name string `json:"name"`
	Description string `json:"description"`
	PriceUSD float64 `json:"price_usd"`
	IncludedCreditUSD float64 `json:"included_credit_usd"`
	ValidDays *int `json:"valid_days"`
	RPMLimit *int `json:"rpm_limit"`
	DailyUSDLimit *float64 `json:"daily_usd_limit"`
	AllowedModels []string `json:"allowed_models"`
	PriceRatio float64 `json:"price_ratio"`
	AllowPaygOverage bool `json:"allow_payg_overage"`
	Enabled bool `json:"enabled"`
	SortOrder int `json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type UserPlanGrant struct {
	ID int64 `json:"id"`
	UserID int64 `json:"user_id"`
	PlanID *int64 `json:"plan_id"`
	PlanNameSnapshot string `json:"plan_name_snapshot"`
	StartsAt time.Time `json:"starts_at"`
	ExpiresAt *time.Time `json:"expires_at"`
	CreditGrantedUSD float64 `json:"credit_granted_usd"`
	CreditRemainingUSD float64 `json:"credit_remaining_usd"`
	PriceRatioSnapshot float64 `json:"price_ratio_snapshot"`
	AllowPaygOverageSnapshot bool `json:"allow_payg_overage_snapshot"`
	Status string `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
```

Implement repository methods:

- `ListPlans(ctx)`
- `ListEnabledPlans(ctx)`
- `CreatePlan(ctx, Plan) (int64, error)`
- `UpdatePlan(ctx, id, Plan) error`
- `GrantPlanToUser(ctx, userID, planID int64, startsAt time.Time) (int64, error)`
- `ActiveGrantForUser(ctx, userID int64, now time.Time) (*UserPlanGrant, error)`
- `ConsumeGrantCredit(ctx, grantID, userID int64, amount float64, requestCreatedAt *time.Time) (float64, error)` returning amount consumed from plan.

- [ ] **Step 4: Run repository tests**

```bash
GOCACHE=$PWD/.codex-gocache go test ./internal/repository -run TestValidatePlan
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/repository/plan.go internal/repository/plan_test.go
git commit -m "feat: add plan repository"
```

---

## Task 5: Plan Admin and Portal APIs

**Files:**
- Create: `internal/handler/admin/plans.go`
- Create: `internal/handler/admin/plans_test.go`
- Create: `internal/handler/portal/plans.go`
- Create: `internal/handler/portal/plans_test.go`
- Modify: `cmd/omnihub/main.go`

- [ ] **Step 1: Write handler tests**

Admin tests:

- `TestCreatePlanRejectsNegativePrice`
- `TestListPlansReturnsRows`
- `TestGrantPlanToUserReturnsGrant`

Portal tests:

- `TestPortalPlansOnlyReturnsEnabledPlans`
- `TestPortalCurrentPlanReturnsActiveGrant`

- [ ] **Step 2: Run tests to verify they fail**

```bash
GOCACHE=$PWD/.codex-gocache go test ./internal/handler/admin ./internal/handler/portal -run Plan
```

Expected: FAIL because handlers do not exist.

- [ ] **Step 3: Implement handlers**

Admin endpoints:

```go
GET   /admin/api/plans
POST  /admin/api/plans
PATCH /admin/api/plans/:id
POST  /admin/api/users/:id/plan-grants
```

Portal endpoints:

```go
GET /portal/api/plans
GET /portal/api/me/plan
POST /portal/api/plans/:id/claim
```

First version claim rules:

- Only plans with `price_usd == 0` can be self-claimed.
- Paid plans return 400 with message `paid plans require admin assignment`.

- [ ] **Step 4: Wire routes**

In `cmd/omnihub/main.go`:

```go
planRepo := repository.NewPlanRepo(pool)
authed.GET("/plans", adminhandler.ListPlansHandler(planRepo))
authed.POST("/plans", adminhandler.CreatePlanHandler(planRepo))
authed.PATCH("/plans/:id", adminhandler.UpdatePlanHandler(planRepo))
authed.POST("/users/:id/plan-grants", adminhandler.GrantPlanToUserHandler(planRepo))
puser.GET("/plans", portalhandler.PlansHandler(planRepo))
puser.GET("/me/plan", portalhandler.CurrentPlanHandler(planRepo))
puser.POST("/plans/:id/claim", portalhandler.ClaimPlanHandler(planRepo))
```

- [ ] **Step 5: Run tests**

```bash
GOCACHE=$PWD/.codex-gocache go test ./internal/handler/admin ./internal/handler/portal
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/handler/admin/plans.go internal/handler/admin/plans_test.go internal/handler/portal/plans.go internal/handler/portal/plans_test.go cmd/omnihub/main.go
git commit -m "feat: add plan APIs"
```

---

## Task 6: Billing Service for Plan-First Charging

**Files:**
- Create: `internal/service/billing/billing.go`
- Create: `internal/service/billing/billing_test.go`
- Modify: `internal/service/limits/balance.go` only if a cache helper is needed.

- [ ] **Step 1: Write billing tests**

Create `internal/service/billing/billing_test.go`:

```go
package billing

import (
	"context"
	"testing"
)

func TestChargeUsesPlanFirstThenWallet(t *testing.T) {
	store := &fakeStore{grantCredit: 3, walletBalance: 10, allowPayg: true}
	svc := New(store)
	res, err := svc.Charge(context.Background(), 7, 5)
	if err != nil { t.Fatal(err) }
	if res.PlanUSD != 3 || res.WalletUSD != 2 { t.Fatalf("got %+v", res) }
}

func TestChargeRejectsWhenPlanExhaustedAndNoOverage(t *testing.T) {
	store := &fakeStore{grantCredit: 1, walletBalance: 10, allowPayg: false}
	svc := New(store)
	if _, err := svc.Charge(context.Background(), 7, 2); err == nil { t.Fatal("expected rejection") }
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
GOCACHE=$PWD/.codex-gocache go test ./internal/service/billing
```

Expected: FAIL because package does not exist.

- [ ] **Step 3: Implement billing service**

Create `internal/service/billing/billing.go`:

```go
package billing

import (
	"context"
	"errors"
	"time"
)

var ErrInsufficientBalance = errors.New("insufficient plan credit or wallet balance")

type ActiveGrant struct {
	ID int64
	UserID int64
	CreditRemainingUSD float64
	AllowPaygOverage bool
}

type Store interface {
	ActiveGrantForUser(ctx context.Context, userID int64, now time.Time) (*ActiveGrant, error)
	ConsumeGrantCredit(ctx context.Context, grantID, userID int64, amount float64, requestCreatedAt *time.Time) (float64, error)
	WalletBalance(ctx context.Context, userID int64) (float64, error)
}

type Result struct { PlanUSD, WalletUSD float64 }

type Service struct { store Store; now func() time.Time }

func New(store Store) *Service { return &Service{store: store, now: time.Now} }

func (s *Service) Charge(ctx context.Context, userID int64, amount float64) (Result, error) {
	if amount <= 0 { return Result{}, nil }
	grant, err := s.store.ActiveGrantForUser(ctx, userID, s.now())
	if err != nil { return Result{}, err }
	remaining := amount
	var out Result
	if grant != nil && grant.CreditRemainingUSD > 0 {
		used, err := s.store.ConsumeGrantCredit(ctx, grant.ID, userID, remaining, nil)
		if err != nil { return Result{}, err }
		out.PlanUSD = used
		remaining -= used
		if remaining <= 0 { return out, nil }
		if !grant.AllowPaygOverage { return Result{}, ErrInsufficientBalance }
	}
	wallet, err := s.store.WalletBalance(ctx, userID)
	if err != nil { return Result{}, err }
	if wallet+1e-9 < remaining { return Result{}, ErrInsufficientBalance }
	out.WalletUSD = remaining
	return out, nil
}
```

- [ ] **Step 4: Run tests**

```bash
GOCACHE=$PWD/.codex-gocache go test ./internal/service/billing
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/service/billing
git commit -m "feat: add plan-first billing service"
```

---

## Task 7: Integrate Billing With Request Admission and Completion

**Files:**
- Modify: `cmd/omnihub/main.go`
- Modify: `internal/handler/gateway/anthropic.go`
- Modify: `internal/handler/gateway/openai.go`
- Modify: `internal/service/limits/balance.go`
- Modify tests under `internal/handler/gateway` or add focused tests.

- [ ] **Step 1: Write tests for request rejection**

Add a test in gateway handler package that sets a fake billing service returning `ErrInsufficientBalance` and verifies response is HTTP 402 or existing prepaid rejection status.

- [ ] **Step 2: Run test to verify it fails**

```bash
GOCACHE=$PWD/.codex-gocache go test ./internal/handler/gateway -run Billing
```

Expected: FAIL because gateway handlers do not accept billing service yet.

- [ ] **Step 3: Wire billing service**

Use a small interface in gateway package:

```go
type BillingCharger interface {
	Charge(ctx context.Context, userID int64, amount float64) (billing.Result, error)
}
```

Integration rule:

- Keep existing pre-request balance guard to avoid obvious negative balances.
- After response cost is known, call billing service with `billed_usd`.
- Existing wallet balance cache charge should only subtract wallet portion, not plan portion.
- Request log should record split fields when message_requests is extended.

- [ ] **Step 4: Run gateway tests**

```bash
GOCACHE=$PWD/.codex-gocache go test ./internal/handler/gateway ./internal/service/limits ./internal/service/billing
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/omnihub/main.go internal/handler/gateway internal/service/limits internal/service/billing
git commit -m "feat: charge requests against plan then wallet"
```

---

## Task 8: Frontend Data Hooks and Navigation

**Files:**
- Create: `web/src/lib/announcements.ts`
- Create: `web/src/lib/plans.ts`
- Modify: `web/src/lib/portalData.ts`
- Modify: `web/src/components/SettingsLayout.tsx`
- Modify: `web/src/components/PortalLayout.tsx`
- Modify: `web/src/App.tsx`
- Modify: locale files.

- [ ] **Step 1: Add data hooks**

`web/src/lib/announcements.ts`:

```ts
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api'
import { papi } from './portalApi'

export interface Announcement { id:number; title:string; body:string; kind:string; status:string; placement:string; priority:number; starts_at:string|null; ends_at:string|null; dismissible:boolean; created_at:string; updated_at:string }

export function useAnnouncements() { return useQuery({ queryKey:['announcements'], queryFn:()=>api<{announcements:Announcement[]}>('/announcements').then(r=>r.announcements) }) }
export function usePortalAnnouncements(placement='portal_home') { return useQuery({ queryKey:['portal-announcements', placement], queryFn:()=>papi<{announcements:Announcement[]}>(`/announcements?placement=${placement}`).then(r=>r.announcements) }) }
export function useSaveAnnouncement() { const qc=useQueryClient(); return useMutation({ mutationFn:(a:Partial<Announcement>)=>api('/announcements',{method:a.id?'PATCH':'POST', body:JSON.stringify(a)}), onSuccess:()=>qc.invalidateQueries({queryKey:['announcements']}) }) }
```

`web/src/lib/plans.ts` includes `Plan`, `UserPlanGrant`, admin hooks and portal hooks.

- [ ] **Step 2: Add routes and nav labels**

Routes:

- `/admin/announcements`
- `/admin/plans`
- `/portal/plans`

Navigation labels:

- zh: 公告、套餐
- en: Announcements, Plans

- [ ] **Step 3: Run build to catch type errors**

```bash
cd web && npm run build
```

Expected: PASS after page components exist in later tasks; until then route imports should not be added.

- [ ] **Step 4: Commit after pages compile**

Do not commit this task until Task 9 and Task 10 page files exist.

---

## Task 9: Admin Announcement and Plan Pages

**Files:**
- Create: `web/src/pages/Announcements.tsx`
- Create: `web/src/pages/Plans.tsx`
- Modify: `web/src/App.tsx`
- Modify locale files.

- [ ] **Step 1: Create `Announcements.tsx`**

Page includes:

- list table: title, kind, placement, status, priority, active window.
- inline form or modal using existing `field`, `btn`, `card` classes.
- actions: save, archive/delete.

- [ ] **Step 2: Create `Plans.tsx`**

Page includes:

- list table: name, price, included credit, valid days, price ratio, overage, enabled.
- form fields: name, description, price, included credit, valid days, RPM, daily USD, allowed models comma-separated, price ratio, allow overage, enabled, sort order.

- [ ] **Step 3: Add routes**

Modify `web/src/App.tsx`:

```tsx
import { AnnouncementsPage } from './pages/Announcements'
import { PlansPage } from './pages/Plans'
// admin routes
<Route path="announcements" element={<AdminProtected><AnnouncementsPage /></AdminProtected>} />
<Route path="plans" element={<AdminProtected><PlansPage /></AdminProtected>} />
```

- [ ] **Step 4: Run build**

```bash
cd web && npm run build
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/Announcements.tsx web/src/pages/Plans.tsx web/src/App.tsx web/src/lib/announcements.ts web/src/lib/plans.ts web/src/lib/locales/en.ts web/src/lib/locales/zh.ts web/src/components/SettingsLayout.tsx
git commit -m "feat: add admin announcement and plan pages"
```

---

## Task 10: Portal Announcement, Current Plan, and Plans Page

**Files:**
- Create: `web/src/pages/portal/PortalPlans.tsx`
- Modify: `web/src/pages/portal/PortalOverview.tsx`
- Modify: `web/src/pages/portal/PortalWallet.tsx`
- Modify: `web/src/components/PortalLayout.tsx`
- Modify: `web/src/App.tsx`
- Modify: `web/src/lib/portalData.ts`
- Modify locale files.

- [ ] **Step 1: Add portal current plan hooks**

In `portalData.ts` add:

```ts
export interface CurrentPlan { grant: UserPlanGrant | null }
export function useCurrentPlan() { return useQuery({ queryKey:['portal-current-plan'], queryFn:()=>papi<CurrentPlan>('/me/plan') }) }
export function usePortalPlans() { return useQuery({ queryKey:['portal-plans'], queryFn:()=>papi<{plans:Plan[]}>('/plans').then(r=>r.plans) }) }
```

- [ ] **Step 2: Portal overview**

Show:

- top announcements list.
- current plan summary: name, remaining credit, expiry, overage behavior.
- wallet balance remains visible.

- [ ] **Step 3: Portal plans page**

Create cards:

- plan name and description.
- price.
- included credit.
- valid days.
- overage allowed or not.
- button: free plan `Claim`, paid plan `Contact admin`.

- [ ] **Step 4: Wallet copy update**

Add text:

- Wallet balance is used for pay-as-you-go usage and plan overage fallback.

- [ ] **Step 5: Build**

```bash
cd web && npm run build
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/pages/portal/PortalPlans.tsx web/src/pages/portal/PortalOverview.tsx web/src/pages/portal/PortalWallet.tsx web/src/components/PortalLayout.tsx web/src/App.tsx web/src/lib/portalData.ts web/src/lib/locales/en.ts web/src/lib/locales/zh.ts
git commit -m "feat: show announcements and plans in portal"
```

---

## Task 11: Full Verification and Docker Rebuild

**Files:**
- No new source files unless earlier tasks require fixes.

- [ ] **Step 1: Format Go files**

```bash
gofmt -w internal/repository/announcement.go internal/repository/plan.go internal/handler/admin/announcements.go internal/handler/admin/plans.go internal/handler/portal/announcements.go internal/handler/portal/plans.go internal/service/billing/billing.go cmd/omnihub/main.go
```

Expected: no output.

- [ ] **Step 2: Run full backend tests**

```bash
GOCACHE=$PWD/.codex-gocache go test -count=1 ./...
```

Expected: every package passes.

- [ ] **Step 3: Run frontend build**

```bash
cd web && npm run build
```

Expected: `tsc -b && vite build` succeeds.

- [ ] **Step 4: Check whitespace and conflict markers**

```bash
git diff --check
```

Expected: no output.

- [ ] **Step 5: Rebuild running service**

```bash
/bin/zsh -lc "HTTP_PROXY=http://127.0.0.1:7897 HTTPS_PROXY=http://127.0.0.1:7897 ALL_PROXY=socks5://127.0.0.1:7897 NO_PROXY=localhost,127.0.0.1,::1 docker compose -f deploy/docker-compose.yaml -f deploy/docker-compose.nginx.yaml --env-file deploy/.env up -d --build postgres omnihub"
```

Expected: gateway recreated and healthy.

- [ ] **Step 6: Smoke test service**

```bash
curl -sS -i http://127.0.0.1:8090/healthz | head -20
```

Expected: HTTP 200 and `{"status":"ok"}`.

- [ ] **Step 7: Browser smoke test**

Use the in-app browser to verify:

- `/admin/announcements` renders.
- `/admin/plans` renders.
- `/portal` shows announcements area.
- `/portal/plans` renders plan cards.

- [ ] **Step 8: Cleanup test cache**

```bash
rm -rf .codex-gocache
```

- [ ] **Step 9: Final commit**

```bash
git status --short
git add internal web cmd docs
 git commit -m "feat: add announcements and billing plans"
```

---

## Self-Review

### Spec coverage

- Announcement admin CRUD: Task 2, Task 3, Task 9.
- Portal announcement display: Task 3, Task 10.
- Plan templates: Task 1, Task 4, Task 5, Task 9.
- User plan grants: Task 1, Task 4, Task 5.
- Plan-first and pay-as-you-go fallback: Task 6, Task 7.
- Wallet remains pay-as-you-go: Task 6, Task 7, Task 10.
- Portal plan display: Task 5, Task 10.
- Verification: Task 11.

### Known sequencing notes

- Task 8 data hooks and navigation should be committed with Task 9/10 pages, because routes should not import missing components.
- Billing integration in Task 7 is the highest-risk part. Keep announcement and plan CRUD merged first so UI work can proceed independently if billing requires extra debugging.
- If plan consumption needs strict financial accuracy beyond first phase, replace cache-only prechecks with DB transaction reservation in a follow-up.
