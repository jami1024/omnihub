// Package pricesync keeps the in-memory price pool in step with the
// model_prices table and syncs that table from LiteLLM's published
// price list. It is the glue between the repository (model_prices) and
// the pricing engine (pricing.Pool); neither of those imports the
// other, so the wiring lives here.
package pricesync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/pricing"
)

// DefaultLiteLLMURL is the canonical community price list. Its JSON keys
// are model names; values carry per-token costs in the same shape as
// pricing.Price. Overridable via OMNIHUB_PRICE_SYNC_URL.
const DefaultLiteLLMURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/litellm/model_prices_and_context_window.json"

// maxBodyBytes caps the download — the LiteLLM file is ~2 MB; 16 MB
// leaves generous headroom while refusing a runaway response.
const maxBodyBytes = 16 << 20

// Refresher rebuilds the pricing.Pool's table from the built-in
// defaults overlaid with the model_prices rows, and re-syncs the table
// from LiteLLM on demand.
type Refresher struct {
	repo *repository.ModelPriceRepo
	pool *pricing.Pool
	base pricing.Table // built-in floor (pricing.Default())
	http *http.Client
}

// New wires a refresher. base is the built-in floor (pricing.Default());
// DB rows overlay it.
func New(repo *repository.ModelPriceRepo, pool *pricing.Pool, base pricing.Table) *Refresher {
	return &Refresher{
		repo: repo,
		pool: pool,
		base: base,
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

// Refresh reads every model_prices row and swaps the pool's live table
// to base-overlaid-with-DB. Called at startup, on NOTIFY, and after a
// sync.
func (r *Refresher) Refresh(ctx context.Context) error {
	rows, err := r.repo.ListAll(ctx)
	if err != nil {
		return err
	}
	over := make(pricing.Table, len(rows))
	for _, m := range rows {
		over[m.Model] = m.Price()
	}
	r.pool.Replace(pricing.Overlay(r.base, over))
	return nil
}

// EnsureSeeded syncs from LiteLLM only when the table is empty — the
// first-boot seed. A failure is logged, not fatal: the gateway still
// prices via the built-in defaults.
func (r *Refresher) EnsureSeeded(ctx context.Context, url string) {
	n, err := r.repo.CountAll(ctx)
	if err != nil {
		slog.Warn("price seed: count failed; skipping", "err", err.Error())
		return
	}
	if n > 0 {
		return
	}
	slog.Info("price table empty; seeding from LiteLLM", "url", url)
	res, err := r.SyncFromLiteLLM(ctx, url)
	if err != nil {
		slog.Warn("price seed from LiteLLM failed; using built-in defaults only", "err", err.Error())
		return
	}
	slog.Info("price table seeded", "added", res.Added, "updated", res.Updated, "skipped", res.Skipped)
}

// SyncFromLiteLLM fetches the price list, upserts every priced model
// into model_prices (source=litellm, never clobbering manual rows), and
// refreshes the live pool. Empty url falls back to DefaultLiteLLMURL.
func (r *Refresher) SyncFromLiteLLM(ctx context.Context, url string) (repository.UpsertResult, error) {
	if url == "" {
		url = DefaultLiteLLMURL
	}
	prices, err := r.fetchLiteLLM(ctx, url)
	if err != nil {
		return repository.UpsertResult{}, err
	}
	res, err := r.repo.UpsertLiteLLM(ctx, prices)
	if err != nil {
		return res, err
	}
	// Reflect the new rows in the live pool immediately. The NOTIFY
	// trigger also fires, but refreshing here makes the API response
	// truthful even if the listener is momentarily disconnected.
	if err := r.Refresh(ctx); err != nil {
		slog.Warn("price pool refresh after sync failed", "err", err.Error())
	}
	return res, nil
}

// liteLLMEntry is the subset of each LiteLLM record we price on. Unknown
// fields are ignored; absent cost fields default to 0 and the pricing
// engine's ratio fallbacks fill cache rates.
type liteLLMEntry struct {
	InputCostPerToken                   float64 `json:"input_cost_per_token"`
	OutputCostPerToken                  float64 `json:"output_cost_per_token"`
	CacheCreationInputTokenCost         float64 `json:"cache_creation_input_token_cost"`
	CacheCreationInputTokenCostAbove1Hr float64 `json:"cache_creation_input_token_cost_above_1hr"`
	CacheReadInputTokenCost             float64 `json:"cache_read_input_token_cost"`
}

func (r *Refresher) fetchLiteLLM(ctx context.Context, url string) ([]repository.ModelPrice, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build price sync request: %w", err)
	}
	resp, err := r.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch price list: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("price list returned HTTP %d", resp.StatusCode)
	}

	var raw map[string]liteLLMEntry
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode price list: %w", err)
	}

	out := make([]repository.ModelPrice, 0, len(raw))
	for model, e := range raw {
		// Skip the documentation stub and any entry with no token price
		// (image-only / embedding-only rows we don't bill on yet).
		if model == "sample_spec" {
			continue
		}
		if e.InputCostPerToken == 0 && e.OutputCostPerToken == 0 {
			continue
		}
		out = append(out, repository.ModelPrice{
			Model:                               model,
			InputCostPerToken:                   e.InputCostPerToken,
			OutputCostPerToken:                  e.OutputCostPerToken,
			CacheCreationInputTokenCost:         e.CacheCreationInputTokenCost,
			CacheCreationInputTokenCostAbove1Hr: e.CacheCreationInputTokenCostAbove1Hr,
			CacheReadInputTokenCost:             e.CacheReadInputTokenCost,
		})
	}
	return out, nil
}
