package admin

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/health"
	"github.com/jami1024/omnihub/internal/service/provider"
)

// CircuitTracker is the slice of the in-memory health.Tracker the admin
// circuit endpoints need. Exported so main can pass the live *Tracker
// (or leave it nil when the gateway is disabled — every handler treats a
// nil tracker as "circuit data unavailable").
type CircuitTracker interface {
	Snapshot(accountID int64) health.Snapshot
	Reset(accountID int64)
}

// circuitAccountLister supplies the account roster the status view walks;
// repository.AccountRepo.ListAll satisfies it.
type circuitAccountLister interface {
	ListAll(ctx context.Context) ([]*provider.Account, []bool, error)
}

// healthEventStore is the read side of account_health_events.
type healthEventStore interface {
	ListRecentAll(ctx context.Context, limit int) ([]repository.AccountHealthEvent, error)
}

// circuitStatusDTO is the live breaker state of one account.
type circuitStatusDTO struct {
	AccountID       int64      `json:"account_id"`
	AccountName     string     `json:"account_name"`
	Enabled         bool       `json:"enabled"`
	State           string     `json:"state"` // closed | open | half-open
	FailureCount    int        `json:"failure_count"`
	LastFailure     *time.Time `json:"last_failure"`
	OpenUntil       *time.Time `json:"open_until"`
	HalfOpenSuccess int        `json:"half_open_success"`
}

// healthEventDTO is one persisted transition.
type healthEventDTO struct {
	CreatedAt    time.Time `json:"created_at"`
	AccountID    int64     `json:"account_id"`
	AccountName  string    `json:"account_name"`
	FromState    string    `json:"from_state"`
	ToState      string    `json:"to_state"`
	FailureCount int       `json:"failure_count"`
	Reason       *string   `json:"reason"`
}

// CircuitStatusHandler returns GET /admin/api/circuit — the live breaker
// state of every account. `available` is false when the gateway (and
// thus the tracker) isn't running, so the UI can explain the empty view.
func CircuitStatusHandler(tracker CircuitTracker, accounts circuitAccountLister) gin.HandlerFunc {
	return func(c *gin.Context) {
		if tracker == nil {
			c.JSON(http.StatusOK, gin.H{"available": false, "accounts": []circuitStatusDTO{}})
			return
		}
		list, enabled, err := accounts.ListAll(c.Request.Context())
		if err != nil {
			slog.Error("admin: circuit status list accounts failed", "err", err.Error())
			writeInternal(c, "could not load accounts")
			return
		}
		out := make([]circuitStatusDTO, len(list))
		for i, a := range list {
			s := tracker.Snapshot(a.ID)
			out[i] = circuitStatusDTO{
				AccountID:       a.ID,
				AccountName:     a.Name,
				Enabled:         enabled[i],
				State:           string(s.State),
				FailureCount:    s.FailureCount,
				LastFailure:     nilIfZero(s.LastFailure),
				OpenUntil:       nilIfZero(s.OpenUntil),
				HalfOpenSuccess: s.HalfOpenSuccess,
			}
		}
		c.JSON(http.StatusOK, gin.H{"available": true, "accounts": out})
	}
}

// CircuitEventsHandler returns GET /admin/api/circuit/events?limit=N —
// the recent state-transition feed across all accounts, newest first.
func CircuitEventsHandler(store healthEventStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := 50
		if raw := c.Query("limit"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 {
				limit = n
			}
		}
		events, err := store.ListRecentAll(c.Request.Context(), limit)
		if err != nil {
			slog.Error("admin: circuit events failed", "err", err.Error())
			writeInternal(c, "could not load circuit events")
			return
		}
		out := make([]healthEventDTO, len(events))
		for i, ev := range events {
			out[i] = healthEventDTO{
				CreatedAt:    ev.CreatedAt,
				AccountID:    ev.AccountID,
				AccountName:  ev.AccountName,
				FromState:    ev.FromState,
				ToState:      ev.ToState,
				FailureCount: ev.FailureCount,
				Reason:       ev.Reason,
			}
		}
		c.JSON(http.StatusOK, gin.H{"events": out})
	}
}

// ResetBreakerHandler handles POST /admin/api/accounts/:id/reset-breaker
// — force an account's breaker back to closed ("the upstream recovered").
func ResetBreakerHandler(tracker CircuitTracker) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseIDParam(c)
		if !ok {
			return
		}
		if tracker == nil {
			writeError(c, http.StatusServiceUnavailable, "unavailable",
				"circuit breaker is not running (gateway disabled)")
			return
		}
		tracker.Reset(id)
		slog.Info("admin: circuit breaker reset", "account_id", id, "admin", adminActor(c))
		c.Status(http.StatusNoContent)
	}
}

func nilIfZero(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
