package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrPlanNotFound = errors.New("plan not found")
var ErrUserPlanGrantNotFound = errors.New("user plan grant not found")

type Plan struct {
	ID                int64     `json:"id"`
	Name              string    `json:"name"`
	Description       string    `json:"description"`
	PriceUSD          float64   `json:"price_usd"`
	IncludedCreditUSD float64   `json:"included_credit_usd"`
	ValidDays         *int      `json:"valid_days"`
	RPMLimit          *int      `json:"rpm_limit"`
	DailyUSDLimit     *float64  `json:"daily_usd_limit"`
	AllowedModels     []string  `json:"allowed_models"`
	PriceRatio        float64   `json:"price_ratio"`
	AllowPaygOverage  bool      `json:"allow_payg_overage"`
	Enabled           bool      `json:"enabled"`
	SortOrder         int       `json:"sort_order"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type UserPlanGrant struct {
	ID                       int64      `json:"id"`
	UserID                   int64      `json:"user_id"`
	PlanID                   *int64     `json:"plan_id"`
	PlanNameSnapshot         string     `json:"plan_name_snapshot"`
	StartsAt                 time.Time  `json:"starts_at"`
	ExpiresAt                *time.Time `json:"expires_at"`
	CreditGrantedUSD         float64    `json:"credit_granted_usd"`
	CreditRemainingUSD       float64    `json:"credit_remaining_usd"`
	PriceRatioSnapshot       float64    `json:"price_ratio_snapshot"`
	AllowPaygOverageSnapshot bool       `json:"allow_payg_overage_snapshot"`
	Status                   string     `json:"status"`
	CreatedAt                time.Time  `json:"created_at"`
	UpdatedAt                time.Time  `json:"updated_at"`
}

type PlanRepo struct {
	pool *pgxpool.Pool
}

func NewPlanRepo(pool *pgxpool.Pool) *PlanRepo {
	return &PlanRepo{pool: pool}
}

func ValidatePlan(p Plan) error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if p.PriceUSD < 0 {
		return fmt.Errorf("price_usd must be greater than or equal to 0")
	}
	if p.IncludedCreditUSD < 0 {
		return fmt.Errorf("included_credit_usd must be greater than or equal to 0")
	}
	if p.ValidDays != nil && *p.ValidDays <= 0 {
		return fmt.Errorf("valid_days must be greater than 0")
	}
	if p.RPMLimit != nil && *p.RPMLimit <= 0 {
		return fmt.Errorf("rpm_limit must be greater than 0")
	}
	if p.DailyUSDLimit != nil && *p.DailyUSDLimit < 0 {
		return fmt.Errorf("daily_usd_limit must be greater than or equal to 0")
	}
	if p.PriceRatio < 0 {
		return fmt.Errorf("price_ratio must be greater than or equal to 0")
	}
	return nil
}

func (r *PlanRepo) ListPlans(ctx context.Context) ([]Plan, error) {
	rows, err := r.pool.Query(ctx, `
        SELECT id, name, description, price_usd::float8, included_credit_usd::float8,
               valid_days, rpm_limit, daily_usd_limit::float8, allowed_models,
               price_ratio::float8, allow_payg_overage, enabled, sort_order,
               created_at, updated_at
          FROM plans
         ORDER BY sort_order ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	defer rows.Close()
	return scanPlans(rows)
}

func (r *PlanRepo) ListEnabledPlans(ctx context.Context) ([]Plan, error) {
	rows, err := r.pool.Query(ctx, `
        SELECT id, name, description, price_usd::float8, included_credit_usd::float8,
               valid_days, rpm_limit, daily_usd_limit::float8, allowed_models,
               price_ratio::float8, allow_payg_overage, enabled, sort_order,
               created_at, updated_at
          FROM plans
         WHERE enabled = TRUE
         ORDER BY sort_order ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list enabled plans: %w", err)
	}
	defer rows.Close()
	return scanPlans(rows)
}

func (r *PlanRepo) GetPlan(ctx context.Context, id int64) (*Plan, error) {
	row := r.pool.QueryRow(ctx, `
        SELECT id, name, description, price_usd::float8, included_credit_usd::float8,
               valid_days, rpm_limit, daily_usd_limit::float8, allowed_models,
               price_ratio::float8, allow_payg_overage, enabled, sort_order,
               created_at, updated_at
          FROM plans
         WHERE id = $1`, id)
	p, err := scanPlan(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPlanNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get plan %d: %w", id, err)
	}
	return p, nil
}

func (r *PlanRepo) CreatePlan(ctx context.Context, p Plan) (int64, error) {
	if err := ValidatePlan(p); err != nil {
		return 0, err
	}
	var id int64
	err := r.pool.QueryRow(ctx, `
        INSERT INTO plans (
            name, description, price_usd, included_credit_usd, valid_days,
            rpm_limit, daily_usd_limit, allowed_models, price_ratio,
            allow_payg_overage, enabled, sort_order
        ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
        RETURNING id`,
		strings.TrimSpace(p.Name), strings.TrimSpace(p.Description), p.PriceUSD,
		p.IncludedCreditUSD, p.ValidDays, p.RPMLimit, p.DailyUSDLimit,
		normalizeAllowedModels(p.AllowedModels), p.PriceRatio, p.AllowPaygOverage,
		p.Enabled, p.SortOrder).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create plan: %w", err)
	}
	return id, nil
}

func (r *PlanRepo) UpdatePlan(ctx context.Context, id int64, p Plan) error {
	if err := ValidatePlan(p); err != nil {
		return err
	}
	ct, err := r.pool.Exec(ctx, `
        UPDATE plans
           SET name = $2,
               description = $3,
               price_usd = $4,
               included_credit_usd = $5,
               valid_days = $6,
               rpm_limit = $7,
               daily_usd_limit = $8,
               allowed_models = $9,
               price_ratio = $10,
               allow_payg_overage = $11,
               enabled = $12,
               sort_order = $13,
               updated_at = NOW()
         WHERE id = $1`,
		id, strings.TrimSpace(p.Name), strings.TrimSpace(p.Description), p.PriceUSD,
		p.IncludedCreditUSD, p.ValidDays, p.RPMLimit, p.DailyUSDLimit,
		normalizeAllowedModels(p.AllowedModels), p.PriceRatio, p.AllowPaygOverage,
		p.Enabled, p.SortOrder)
	if err != nil {
		return fmt.Errorf("update plan %d: %w", id, err)
	}
	if ct.RowsAffected() == 0 {
		return ErrPlanNotFound
	}
	return nil
}

func (r *PlanRepo) GrantPlanToUser(ctx context.Context, userID, planID int64, startsAt time.Time) (int64, error) {
	plan, err := r.GetPlan(ctx, planID)
	if err != nil {
		return 0, err
	}
	var expiresAt *time.Time
	if plan.ValidDays != nil {
		expires := startsAt.AddDate(0, 0, *plan.ValidDays)
		expiresAt = &expires
	}
	var id int64
	err = r.pool.QueryRow(ctx, `
        INSERT INTO user_plan_grants (
            user_id, plan_id, plan_name_snapshot, starts_at, expires_at,
            credit_granted_usd, credit_remaining_usd, price_ratio_snapshot,
            allow_payg_overage_snapshot, status
        ) VALUES ($1,$2,$3,$4,$5,$6,$6,$7,$8,'active')
        RETURNING id`,
		userID, plan.ID, plan.Name, startsAt, expiresAt, plan.IncludedCreditUSD,
		plan.PriceRatio, plan.AllowPaygOverage).Scan(&id)
	if err != nil {
		if isForeignKeyViolation(err) {
			return 0, ErrUserNotFound
		}
		return 0, fmt.Errorf("grant plan %d to user %d: %w", planID, userID, err)
	}
	return id, nil
}

func (r *PlanRepo) ActiveGrantForUser(ctx context.Context, userID int64, now time.Time) (*UserPlanGrant, error) {
	row := r.pool.QueryRow(ctx, `
        SELECT id, user_id, plan_id, plan_name_snapshot, starts_at, expires_at,
               credit_granted_usd::float8, credit_remaining_usd::float8,
               price_ratio_snapshot::float8, allow_payg_overage_snapshot,
               status, created_at, updated_at
          FROM user_plan_grants
         WHERE user_id = $1
           AND status IN ('active', 'depleted')
           AND starts_at <= $2
           AND (expires_at IS NULL OR expires_at > $2)
         ORDER BY starts_at DESC, id DESC
         LIMIT 1`, userID, now)
	g, err := scanUserPlanGrant(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("active grant for user %d: %w", userID, err)
	}
	return g, nil
}

func (r *PlanRepo) ListGrantsByUser(ctx context.Context, userID int64) ([]UserPlanGrant, error) {
	rows, err := r.pool.Query(ctx, `
        SELECT id, user_id, plan_id, plan_name_snapshot, starts_at, expires_at,
               credit_granted_usd::float8, credit_remaining_usd::float8,
               price_ratio_snapshot::float8, allow_payg_overage_snapshot,
               status, created_at, updated_at
          FROM user_plan_grants
         WHERE user_id = $1
         ORDER BY starts_at DESC, id DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list grants for user %d: %w", userID, err)
	}
	defer rows.Close()
	return scanUserPlanGrants(rows)
}

func (r *PlanRepo) ConsumeGrantCredit(ctx context.Context, grantID, userID int64, amount float64, requestCreatedAt *time.Time) (float64, error) {
	if amount <= 0 {
		return 0, nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin consume grant credit: %w", err)
	}
	defer tx.Rollback(ctx)

	// Lock the active grant, then deduct in SQL numeric and derive the
	// consumed amount as (old − new) at the column's stored 6-dp scale. Doing
	// the arithmetic in Postgres (rather than reading ::float8, subtracting in
	// Go, and writing back) keeps the ledger amount and credit_remaining
	// rounded identically, so the ledger always sums to granted − remaining
	// with no float drift.
	var consumed float64
	err = tx.QueryRow(ctx, `
        WITH locked AS (
            SELECT credit_remaining_usd AS rem
              FROM user_plan_grants
             WHERE id = $1 AND user_id = $2 AND status = 'active'
             FOR UPDATE
        ), upd AS (
            UPDATE user_plan_grants g
               SET credit_remaining_usd = GREATEST(l.rem - $3::numeric, 0),
                   status = CASE WHEN GREATEST(l.rem - $3::numeric, 0) <= 0 THEN 'depleted' ELSE 'active' END,
                   updated_at = NOW()
              FROM locked l
             WHERE g.id = $1 AND g.user_id = $2
            RETURNING (l.rem - g.credit_remaining_usd)::float8 AS consumed
        )
        SELECT consumed FROM upd`, grantID, userID, amount).Scan(&consumed)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("consume grant credit: %w", err)
	}
	if consumed <= 0 {
		return 0, nil
	}
	if _, err := tx.Exec(ctx, `
        INSERT INTO plan_usage_ledger (user_plan_grant_id, user_id, amount_usd, request_created_at, note)
        VALUES ($1, $2, $3, $4, 'request usage')`, grantID, userID, consumed, requestCreatedAt); err != nil {
		return 0, fmt.Errorf("insert plan usage ledger: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit consume grant credit: %w", err)
	}
	return consumed, nil
}

func scanPlans(rows pgx.Rows) ([]Plan, error) {
	out := []Plan{}
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func scanPlan(row interface{ Scan(...any) error }) (*Plan, error) {
	var p Plan
	if err := row.Scan(
		&p.ID, &p.Name, &p.Description, &p.PriceUSD, &p.IncludedCreditUSD,
		&p.ValidDays, &p.RPMLimit, &p.DailyUSDLimit, &p.AllowedModels,
		&p.PriceRatio, &p.AllowPaygOverage, &p.Enabled, &p.SortOrder,
		&p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if p.AllowedModels == nil {
		p.AllowedModels = []string{}
	}
	return &p, nil
}

func scanUserPlanGrants(rows pgx.Rows) ([]UserPlanGrant, error) {
	out := []UserPlanGrant{}
	for rows.Next() {
		g, err := scanUserPlanGrant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *g)
	}
	return out, rows.Err()
}

func scanUserPlanGrant(row interface{ Scan(...any) error }) (*UserPlanGrant, error) {
	var g UserPlanGrant
	if err := row.Scan(
		&g.ID, &g.UserID, &g.PlanID, &g.PlanNameSnapshot, &g.StartsAt, &g.ExpiresAt,
		&g.CreditGrantedUSD, &g.CreditRemainingUSD, &g.PriceRatioSnapshot,
		&g.AllowPaygOverageSnapshot, &g.Status, &g.CreatedAt, &g.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &g, nil
}

func normalizeAllowedModels(models []string) []string {
	if len(models) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(models))
	seen := map[string]struct{}{}
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		out = append(out, model)
	}
	return out
}
