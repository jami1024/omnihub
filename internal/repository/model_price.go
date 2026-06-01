package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jami1024/omnihub/internal/service/pricing"
)

// ModelPriceRepo persists the model_prices table — the data-backed
// pricing layer that overlays the built-in pricing.Default() table.
type ModelPriceRepo struct {
	pool *pgxpool.Pool
}

// NewModelPriceRepo wires the repository onto an existing pgx pool.
func NewModelPriceRepo(pool *pgxpool.Pool) *ModelPriceRepo {
	return &ModelPriceRepo{pool: pool}
}

// Price source values. 'litellm' rows are owned by the sync and get
// overwritten on each pass; 'manual' rows are operator edits the sync
// must never clobber.
const (
	PriceSourceLiteLLM = "litellm"
	PriceSourceManual  = "manual"
)

// ErrModelPriceNotFound is returned when a single-row lookup misses.
var ErrModelPriceNotFound = errors.New("model price not found")

// ErrModelPriceExists is returned when an insert collides with the
// UNIQUE(model) constraint.
var ErrModelPriceExists = errors.New("a price for that model already exists")

// ModelPrice is one row of model_prices. The cost fields mirror
// pricing.Price (USD per token).
type ModelPrice struct {
	ID                                  int64
	Model                               string
	InputCostPerToken                   float64
	OutputCostPerToken                  float64
	CacheCreationInputTokenCost         float64
	CacheCreationInputTokenCostAbove1Hr float64
	CacheReadInputTokenCost             float64
	Source                              string
	CreatedAt                           time.Time
	UpdatedAt                           time.Time
}

// Price projects the row onto the pricing-engine shape.
func (m ModelPrice) Price() pricing.Price {
	return pricing.Price{
		InputCostPerToken:                   m.InputCostPerToken,
		OutputCostPerToken:                  m.OutputCostPerToken,
		CacheCreationInputTokenCost:         m.CacheCreationInputTokenCost,
		CacheCreationInputTokenCostAbove1Hr: m.CacheCreationInputTokenCostAbove1Hr,
		CacheReadInputTokenCost:             m.CacheReadInputTokenCost,
	}
}

// ModelPriceParams carries the mutable cost columns for insert/update.
type ModelPriceParams struct {
	InputCostPerToken                   float64
	OutputCostPerToken                  float64
	CacheCreationInputTokenCost         float64
	CacheCreationInputTokenCostAbove1Hr float64
	CacheReadInputTokenCost             float64
}

const modelPriceColumns = `id, model,
        input_cost_per_token, output_cost_per_token,
        cache_creation_input_token_cost, cache_creation_input_token_cost_above_1hr,
        cache_read_input_token_cost, source, created_at, updated_at`

func scanModelPrice(row interface{ Scan(...any) error }) (ModelPrice, error) {
	var m ModelPrice
	err := row.Scan(
		&m.ID, &m.Model,
		&m.InputCostPerToken, &m.OutputCostPerToken,
		&m.CacheCreationInputTokenCost, &m.CacheCreationInputTokenCostAbove1Hr,
		&m.CacheReadInputTokenCost, &m.Source, &m.CreatedAt, &m.UpdatedAt,
	)
	return m, err
}

// ListAll returns every price row, ordered by model. Feeds both the
// in-memory pool and the admin UI.
func (r *ModelPriceRepo) ListAll(ctx context.Context) ([]ModelPrice, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+modelPriceColumns+` FROM model_prices ORDER BY model ASC`)
	if err != nil {
		return nil, fmt.Errorf("query model_prices: %w", err)
	}
	defer rows.Close()

	var out []ModelPrice
	for rows.Next() {
		m, err := scanModelPrice(rows)
		if err != nil {
			return nil, fmt.Errorf("scan model_price: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CountAll reports the row count — used by the startup seed to detect an
// empty table.
func (r *ModelPriceRepo) CountAll(ctx context.Context) (int, error) {
	var n int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_prices`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count model_prices: %w", err)
	}
	return n, nil
}

// GetByID fetches one row by primary key.
func (r *ModelPriceRepo) GetByID(ctx context.Context, id int64) (ModelPrice, error) {
	m, err := scanModelPrice(r.pool.QueryRow(ctx, `SELECT `+modelPriceColumns+` FROM model_prices WHERE id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ModelPrice{}, ErrModelPriceNotFound
		}
		return ModelPrice{}, fmt.Errorf("query model_price %d: %w", id, err)
	}
	return m, nil
}

// InsertManual creates an operator-defined price (source = manual).
func (r *ModelPriceRepo) InsertManual(ctx context.Context, model string, p ModelPriceParams) (int64, error) {
	const q = `
        INSERT INTO model_prices (
            model, input_cost_per_token, output_cost_per_token,
            cache_creation_input_token_cost, cache_creation_input_token_cost_above_1hr,
            cache_read_input_token_cost, source
        ) VALUES ($1,$2,$3,$4,$5,$6,'manual')
        RETURNING id`
	var id int64
	err := r.pool.QueryRow(ctx, q,
		model, p.InputCostPerToken, p.OutputCostPerToken,
		p.CacheCreationInputTokenCost, p.CacheCreationInputTokenCostAbove1Hr,
		p.CacheReadInputTokenCost,
	).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, ErrModelPriceExists
		}
		return 0, fmt.Errorf("insert model_price %q: %w", model, err)
	}
	return id, nil
}

// UpdateManual replaces the cost columns of a row and stamps it
// 'manual' — an operator edit takes ownership away from the sync.
func (r *ModelPriceRepo) UpdateManual(ctx context.Context, id int64, p ModelPriceParams) error {
	const q = `
        UPDATE model_prices SET
            input_cost_per_token = $2, output_cost_per_token = $3,
            cache_creation_input_token_cost = $4,
            cache_creation_input_token_cost_above_1hr = $5,
            cache_read_input_token_cost = $6,
            source = 'manual', updated_at = NOW()
         WHERE id = $1`
	tag, err := r.pool.Exec(ctx, q, id,
		p.InputCostPerToken, p.OutputCostPerToken,
		p.CacheCreationInputTokenCost, p.CacheCreationInputTokenCostAbove1Hr,
		p.CacheReadInputTokenCost,
	)
	if err != nil {
		return fmt.Errorf("update model_price %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrModelPriceNotFound
	}
	return nil
}

// DeleteByID removes a price row.
func (r *ModelPriceRepo) DeleteByID(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM model_prices WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete model_price %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrModelPriceNotFound
	}
	return nil
}

// UpsertResult reports what a sync pass changed.
type UpsertResult struct {
	Added   int
	Updated int
	Skipped int // manual rows the sync left untouched
}

// UpsertLiteLLM bulk-writes synced prices. For each model it inserts a
// new 'litellm' row or updates an existing 'litellm' row, but NEVER
// touches a 'manual' row — operator overrides win. Runs in one
// transaction so a mid-sync failure leaves the table unchanged.
func (r *ModelPriceRepo) UpsertLiteLLM(ctx context.Context, prices []ModelPrice) (UpsertResult, error) {
	var res UpsertResult
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return res, fmt.Errorf("begin sync tx: %w", err)
	}
	defer tx.Rollback(ctx)

	const q = `
        INSERT INTO model_prices (
            model, input_cost_per_token, output_cost_per_token,
            cache_creation_input_token_cost, cache_creation_input_token_cost_above_1hr,
            cache_read_input_token_cost, source
        ) VALUES ($1,$2,$3,$4,$5,$6,'litellm')
        ON CONFLICT (model) DO UPDATE SET
            input_cost_per_token = EXCLUDED.input_cost_per_token,
            output_cost_per_token = EXCLUDED.output_cost_per_token,
            cache_creation_input_token_cost = EXCLUDED.cache_creation_input_token_cost,
            cache_creation_input_token_cost_above_1hr = EXCLUDED.cache_creation_input_token_cost_above_1hr,
            cache_read_input_token_cost = EXCLUDED.cache_read_input_token_cost,
            updated_at = NOW()
        WHERE model_prices.source <> 'manual'
        RETURNING (xmax = 0) AS inserted`
	for _, m := range prices {
		var inserted bool
		err := tx.QueryRow(ctx, q,
			m.Model, m.InputCostPerToken, m.OutputCostPerToken,
			m.CacheCreationInputTokenCost, m.CacheCreationInputTokenCostAbove1Hr,
			m.CacheReadInputTokenCost,
		).Scan(&inserted)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// ON CONFLICT matched a 'manual' row; the WHERE blocked the
			// update, so nothing was written.
			res.Skipped++
		case err != nil:
			return res, fmt.Errorf("upsert model_price %q: %w", m.Model, err)
		case inserted:
			res.Added++
		default:
			res.Updated++
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return res, fmt.Errorf("commit sync tx: %w", err)
	}
	return res, nil
}
