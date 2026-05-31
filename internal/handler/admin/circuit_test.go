package admin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	handler "github.com/jami1024/omnihub/internal/handler/admin"
	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/health"
	"github.com/jami1024/omnihub/internal/service/provider"
)

type fakeTracker struct {
	snaps     map[int64]health.Snapshot
	resetCall int64
}

func (f *fakeTracker) Snapshot(id int64) health.Snapshot {
	if s, ok := f.snaps[id]; ok {
		return s
	}
	return health.Snapshot{State: health.StateClosed}
}
func (f *fakeTracker) Reset(id int64) { f.resetCall = id }

type fakeAccountLister struct {
	list    []*provider.Account
	enabled []bool
}

func (f *fakeAccountLister) ListAll(context.Context) ([]*provider.Account, []bool, error) {
	return f.list, f.enabled, nil
}

type fakeEventStore struct {
	events    []repository.AccountHealthEvent
	lastLimit int
}

func (f *fakeEventStore) ListRecentAll(_ context.Context, limit int) ([]repository.AccountHealthEvent, error) {
	f.lastLimit = limit
	return f.events, nil
}

func TestCircuitStatusReportsLiveState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tracker := &fakeTracker{snaps: map[int64]health.Snapshot{
		2: {State: health.StateOpen, FailureCount: 5, OpenUntil: time.Now().Add(time.Minute)},
	}}
	accounts := &fakeAccountLister{
		list:    []*provider.Account{{ID: 1, Name: "a"}, {ID: 2, Name: "b"}},
		enabled: []bool{true, true},
	}
	r := gin.New()
	r.GET("/admin/api/circuit", handler.CircuitStatusHandler(tracker, accounts))

	rec := do(r, http.MethodGet, "/admin/api/circuit", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Available bool `json:"available"`
		Accounts  []struct {
			AccountID int64  `json:"account_id"`
			State     string `json:"state"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Available {
		t.Error("available should be true when tracker present")
	}
	if len(resp.Accounts) != 2 {
		t.Fatalf("accounts = %d, want 2", len(resp.Accounts))
	}
	if resp.Accounts[0].State != "closed" {
		t.Errorf("account 1 state = %q, want closed (never recorded)", resp.Accounts[0].State)
	}
	if resp.Accounts[1].State != "open" {
		t.Errorf("account 2 state = %q, want open", resp.Accounts[1].State)
	}
}

func TestCircuitStatusUnavailableWhenNilTracker(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// nil interface → gateway disabled.
	r.GET("/admin/api/circuit", handler.CircuitStatusHandler(nil, &fakeAccountLister{}))
	rec := do(r, http.MethodGet, "/admin/api/circuit", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp struct {
		Available bool `json:"available"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Available {
		t.Error("available should be false when tracker is nil")
	}
}

func TestResetBreaker(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tracker := &fakeTracker{}
	r := gin.New()
	r.POST("/admin/api/accounts/:id/reset-breaker", handler.ResetBreakerHandler(tracker))
	rec := do(r, http.MethodPost, "/admin/api/accounts/9/reset-breaker", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body %s)", rec.Code, rec.Body.String())
	}
	if tracker.resetCall != 9 {
		t.Errorf("reset called for id %d, want 9", tracker.resetCall)
	}
}

func TestResetBreakerUnavailableWhenNilTracker(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/admin/api/accounts/:id/reset-breaker", handler.ResetBreakerHandler(nil))
	rec := do(r, http.MethodPost, "/admin/api/accounts/9/reset-breaker", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestCircuitEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reason := "upstream 529"
	store := &fakeEventStore{events: []repository.AccountHealthEvent{
		{AccountID: 1, AccountName: "a", FromState: "closed", ToState: "open", FailureCount: 5, Reason: &reason},
	}}
	r := gin.New()
	r.GET("/admin/api/circuit/events", handler.CircuitEventsHandler(store))
	rec := do(r, http.MethodGet, "/admin/api/circuit/events?limit=10", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if store.lastLimit != 10 {
		t.Errorf("limit = %d, want 10", store.lastLimit)
	}
	var resp struct {
		Events []struct {
			ToState string  `json:"to_state"`
			Reason  *string `json:"reason"`
		} `json:"events"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Events) != 1 || resp.Events[0].ToState != "open" {
		t.Fatalf("unexpected events: %+v", resp.Events)
	}
	if resp.Events[0].Reason == nil || *resp.Events[0].Reason != reason {
		t.Errorf("reason not forwarded: %v", resp.Events[0].Reason)
	}
}
