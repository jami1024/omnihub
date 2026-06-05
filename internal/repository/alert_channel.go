package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jami1024/omnihub/internal/crypto"
	"github.com/jami1024/omnihub/internal/service/alertchannel"
)

// AlertChannelRepo persists the alert_channels table. The url column
// (often carrying an embedded token) is encrypted at rest with the
// injected cipher, exactly like account credentials.
type AlertChannelRepo struct {
	pool   *pgxpool.Pool
	cipher *crypto.Cipher
}

// NewAlertChannelRepo wires the repository onto an existing pgx pool.
func NewAlertChannelRepo(pool *pgxpool.Pool, cipher *crypto.Cipher) *AlertChannelRepo {
	return &AlertChannelRepo{pool: pool, cipher: cipher}
}

// ErrAlertChannelNotFound is returned when an update/delete/get misses.
var ErrAlertChannelNotFound = errors.New("alert channel not found")

// ListEnabled returns the enabled channels with decrypted URLs. Feeds the
// in-memory pool that the Alerter delivers through.
func (r *AlertChannelRepo) ListEnabled(ctx context.Context) ([]alertchannel.Channel, error) {
	const q = `
        SELECT id, type, name, url, enabled
          FROM alert_channels
         WHERE enabled = TRUE
         ORDER BY id`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query alert_channels: %w", err)
	}
	defer rows.Close()

	var out []alertchannel.Channel
	for rows.Next() {
		var c alertchannel.Channel
		if err := rows.Scan(&c.ID, &c.Type, &c.Name, &c.URL, &c.Enabled); err != nil {
			return nil, fmt.Errorf("scan alert_channels: %w", err)
		}
		url, err := r.cipher.DecryptString(c.URL)
		if err != nil {
			return nil, fmt.Errorf("decrypt url for channel %d: %w", c.ID, err)
		}
		c.URL = url
		out = append(out, c)
	}
	return out, rows.Err()
}

// AlertChannelRecord is the admin-facing view of one row. The url is
// deliberately omitted — it is a write-only secret, surfaced only to the
// gateway's delivery path, never back to the UI.
type AlertChannelRecord struct {
	ID        int64
	Type      string
	Name      string
	Enabled   bool
	CreatedAt time.Time
	CreatedBy string
}

// AlertChannelParams carries the mutable columns. On update an empty URL
// keeps the stored value (the UI sends it blank to leave the secret as-is).
type AlertChannelParams struct {
	Type    string
	Name    string
	URL     string
	Enabled bool
}

// ListRecords returns every channel (without the url) newest first.
func (r *AlertChannelRepo) ListRecords(ctx context.Context) ([]AlertChannelRecord, error) {
	const q = `
        SELECT id, type, name, enabled, created_at, COALESCE(created_by, '')
          FROM alert_channels
         ORDER BY created_at DESC`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query alert_channels: %w", err)
	}
	defer rows.Close()

	var out []AlertChannelRecord
	for rows.Next() {
		var rec AlertChannelRecord
		if err := rows.Scan(&rec.ID, &rec.Type, &rec.Name, &rec.Enabled, &rec.CreatedAt, &rec.CreatedBy); err != nil {
			return nil, fmt.Errorf("scan alert_channels: %w", err)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// Insert adds a channel, encrypting the url. Returns the new id.
func (r *AlertChannelRepo) Insert(ctx context.Context, p AlertChannelParams, createdBy string) (int64, error) {
	enc, err := r.cipher.EncryptString(p.URL)
	if err != nil {
		return 0, fmt.Errorf("encrypt url: %w", err)
	}
	const q = `
        INSERT INTO alert_channels (type, name, url, enabled, created_by)
        VALUES ($1, $2, $3, $4, NULLIF($5, ''))
        RETURNING id`
	var id int64
	if err := r.pool.QueryRow(ctx, q, p.Type, p.Name, enc, p.Enabled, createdBy).Scan(&id); err != nil {
		return 0, fmt.Errorf("insert alert_channel: %w", err)
	}
	return id, nil
}

// Update replaces the mutable columns of one channel. An empty URL leaves
// the stored (encrypted) value untouched. Returns ErrAlertChannelNotFound
// when no row matches.
func (r *AlertChannelRepo) Update(ctx context.Context, id int64, p AlertChannelParams) error {
	var enc string
	if p.URL != "" {
		var err error
		if enc, err = r.cipher.EncryptString(p.URL); err != nil {
			return fmt.Errorf("encrypt url: %w", err)
		}
	}
	const q = `
        UPDATE alert_channels SET
            type = $2, name = $3, enabled = $4,
            url = COALESCE(NULLIF($5, ''), url)
         WHERE id = $1`
	tag, err := r.pool.Exec(ctx, q, id, p.Type, p.Name, p.Enabled, enc)
	if err != nil {
		return fmt.Errorf("update alert_channel %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAlertChannelNotFound
	}
	return nil
}

// Delete removes a channel. Returns ErrAlertChannelNotFound when missing.
func (r *AlertChannelRepo) Delete(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM alert_channels WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete alert_channel %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAlertChannelNotFound
	}
	return nil
}

// Get returns one channel with its decrypted URL. Used by the admin
// test-send endpoint. Returns ErrAlertChannelNotFound when missing.
func (r *AlertChannelRepo) Get(ctx context.Context, id int64) (alertchannel.Channel, error) {
	const q = `SELECT id, type, name, url, enabled FROM alert_channels WHERE id = $1`
	var c alertchannel.Channel
	err := r.pool.QueryRow(ctx, q, id).Scan(&c.ID, &c.Type, &c.Name, &c.URL, &c.Enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrAlertChannelNotFound
	}
	if err != nil {
		return c, fmt.Errorf("get alert_channel %d: %w", id, err)
	}
	url, err := r.cipher.DecryptString(c.URL)
	if err != nil {
		return c, fmt.Errorf("decrypt url for channel %d: %w", id, err)
	}
	c.URL = url
	return c, nil
}
