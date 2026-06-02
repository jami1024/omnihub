package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PortalSettingsRepo reads/writes the single-row portal_settings table —
// the admin-controlled policy for the end-user portal.
type PortalSettingsRepo struct {
	pool *pgxpool.Pool
}

// NewPortalSettingsRepo wires the repo onto an existing pool.
func NewPortalSettingsRepo(pool *pgxpool.Pool) *PortalSettingsRepo {
	return &PortalSettingsRepo{pool: pool}
}

// PortalSettings is the portal policy. Nil limit pointers mean "no
// default" / "no cap".
type PortalSettings struct {
	SignupEnabled      bool     `json:"signup_enabled"`
	KeyDailyUSDDefault *float64 `json:"key_daily_usd_default"`
	KeyDailyUSDMax     *float64 `json:"key_daily_usd_max"`
	KeyRPMMax          *int     `json:"key_rpm_max"`
}

// Get returns the current settings. The row is seeded by the migration,
// so this always finds it; a missing row falls back to permissive
// defaults (signup on, no caps) rather than erroring.
func (r *PortalSettingsRepo) Get(ctx context.Context) (PortalSettings, error) {
	var s PortalSettings
	err := r.pool.QueryRow(ctx, `
        SELECT signup_enabled, key_daily_usd_default, key_daily_usd_max, key_rpm_max
          FROM portal_settings WHERE id = 1`).
		Scan(&s.SignupEnabled, &s.KeyDailyUSDDefault, &s.KeyDailyUSDMax, &s.KeyRPMMax)
	if err != nil {
		return PortalSettings{SignupEnabled: true}, fmt.Errorf("read portal_settings: %w", err)
	}
	return s, nil
}

// Update replaces the policy (upserting the single row).
func (r *PortalSettingsRepo) Update(ctx context.Context, s PortalSettings) error {
	_, err := r.pool.Exec(ctx, `
        INSERT INTO portal_settings (id, signup_enabled, key_daily_usd_default, key_daily_usd_max, key_rpm_max, updated_at)
        VALUES (1, $1, $2, $3, $4, NOW())
        ON CONFLICT (id) DO UPDATE SET
            signup_enabled = EXCLUDED.signup_enabled,
            key_daily_usd_default = EXCLUDED.key_daily_usd_default,
            key_daily_usd_max = EXCLUDED.key_daily_usd_max,
            key_rpm_max = EXCLUDED.key_rpm_max,
            updated_at = NOW()`,
		s.SignupEnabled, s.KeyDailyUSDDefault, s.KeyDailyUSDMax, s.KeyRPMMax)
	if err != nil {
		return fmt.Errorf("update portal_settings: %w", err)
	}
	return nil
}
