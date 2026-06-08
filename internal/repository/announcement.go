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

type AnnouncementRepo struct {
	pool *pgxpool.Pool
}

func NewAnnouncementRepo(pool *pgxpool.Pool) *AnnouncementRepo {
	return &AnnouncementRepo{pool: pool}
}

func ValidateAnnouncement(a Announcement) error {
	if strings.TrimSpace(a.Title) == "" {
		return fmt.Errorf("title is required")
	}
	if strings.TrimSpace(a.Body) == "" {
		return fmt.Errorf("body is required")
	}
	if !oneOfAnnouncement(a.Kind, "info", "maintenance", "pricing", "model") {
		return fmt.Errorf("invalid kind")
	}
	if !oneOfAnnouncement(a.Status, "draft", "published", "archived") {
		return fmt.Errorf("invalid status")
	}
	if !oneOfAnnouncement(a.Placement, "portal_home", "login", "banner") {
		return fmt.Errorf("invalid placement")
	}
	return nil
}

func oneOfAnnouncement(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

func (r *AnnouncementRepo) List(ctx context.Context) ([]Announcement, error) {
	rows, err := r.pool.Query(ctx, `
        SELECT id, title, body, kind, status, placement, priority,
               starts_at, ends_at, dismissible, created_at, updated_at
          FROM announcements
         ORDER BY priority DESC, created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list announcements: %w", err)
	}
	defer rows.Close()
	return scanAnnouncements(rows)
}

func (r *AnnouncementRepo) ListActive(ctx context.Context, placement string, now time.Time) ([]Announcement, error) {
	rows, err := r.pool.Query(ctx, `
        SELECT id, title, body, kind, status, placement, priority,
               starts_at, ends_at, dismissible, created_at, updated_at
          FROM announcements
         WHERE status = 'published'
           AND placement = $1
           AND (starts_at IS NULL OR starts_at <= $2)
           AND (ends_at IS NULL OR ends_at > $2)
         ORDER BY priority DESC, created_at DESC`,
		placement, now)
	if err != nil {
		return nil, fmt.Errorf("list active announcements: %w", err)
	}
	defer rows.Close()
	return scanAnnouncements(rows)
}

func (r *AnnouncementRepo) Create(ctx context.Context, a Announcement) (int64, error) {
	if err := ValidateAnnouncement(a); err != nil {
		return 0, err
	}
	var id int64
	err := r.pool.QueryRow(ctx, `
        INSERT INTO announcements (
            title, body, kind, status, placement, priority,
            starts_at, ends_at, dismissible
        )
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
        RETURNING id`,
		strings.TrimSpace(a.Title), strings.TrimSpace(a.Body), a.Kind, a.Status,
		a.Placement, a.Priority, a.StartsAt, a.EndsAt, a.Dismissible).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create announcement: %w", err)
	}
	return id, nil
}

func (r *AnnouncementRepo) Update(ctx context.Context, id int64, a Announcement) error {
	if err := ValidateAnnouncement(a); err != nil {
		return err
	}
	ct, err := r.pool.Exec(ctx, `
        UPDATE announcements
           SET title = $2,
               body = $3,
               kind = $4,
               status = $5,
               placement = $6,
               priority = $7,
               starts_at = $8,
               ends_at = $9,
               dismissible = $10,
               updated_at = NOW()
         WHERE id = $1`,
		id, strings.TrimSpace(a.Title), strings.TrimSpace(a.Body), a.Kind, a.Status,
		a.Placement, a.Priority, a.StartsAt, a.EndsAt, a.Dismissible)
	if err != nil {
		return fmt.Errorf("update announcement: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrAnnouncementNotFound
	}
	return nil
}

func (r *AnnouncementRepo) Delete(ctx context.Context, id int64) error {
	ct, err := r.pool.Exec(ctx, `DELETE FROM announcements WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete announcement: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrAnnouncementNotFound
	}
	return nil
}

func scanAnnouncements(rows pgx.Rows) ([]Announcement, error) {
	out := []Announcement{}
	for rows.Next() {
		var a Announcement
		if err := rows.Scan(
			&a.ID, &a.Title, &a.Body, &a.Kind, &a.Status, &a.Placement, &a.Priority,
			&a.StartsAt, &a.EndsAt, &a.Dismissible, &a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan announcement: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
