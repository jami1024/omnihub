package portal_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/handler/portal"
	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/apikey"
	"github.com/jami1024/omnihub/internal/service/pricing"
)

// fakeRequestStore records the key names it was scoped to.
type fakeRequestStore struct {
	gotNames []string
	rows     []repository.RequestLogRow
	total    int
}

func (f *fakeRequestStore) ListByKeyNames(_ context.Context, names []string, _ time.Time, _, _ int) ([]repository.RequestLogRow, int, error) {
	f.gotNames = names
	return f.rows, f.total, nil
}

func TestRequestsHandlerScopesToUserKeys(t *testing.T) {
	billed := 0.018
	keys := &fakeKeyStore{list: []*apikey.Key{
		{ID: 1, Name: "alice-1", Enabled: true},
		{ID: 2, Name: "alice-2", Enabled: true},
	}}
	store := &fakeRequestStore{
		rows: []repository.RequestLogRow{{
			KeyName:                  "alice-1",
			Model:                    "m",
			InputTokens:              5,
			CacheCreationInputTokens: 7,
			CacheReadInputTokens:     11,
			CostUSD:                  0.015,
			BilledUSD:                &billed,
			CostBreakdown: &pricing.Breakdown{
				Input:      0.003,
				Output:     0.012,
				Total:      0.015,
				Multiplier: 1.5,
			},
		}},
		total: 1,
	}
	r := engineWithUser(func(r *gin.Engine) {
		r.GET("/requests", portal.RequestsHandler(store, keys))
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/requests?days=7&page=1", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	// The store MUST be scoped to exactly the user's own key names.
	if len(store.gotNames) != 2 || store.gotNames[0] != "alice-1" || store.gotNames[1] != "alice-2" {
		t.Errorf("store scoped to %v, want [alice-1 alice-2]", store.gotNames)
	}
	var body struct {
		Total    int              `json:"total"`
		Requests []map[string]any `json:"requests"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.Total != 1 || len(body.Requests) != 1 || body.Requests[0]["key_name"] != "alice-1" {
		t.Errorf("unexpected body: %s", w.Body.String())
	}
	if got := body.Requests[0]["billed_usd"]; got != 0.018 {
		t.Errorf("billed_usd = %#v, want 0.018", got)
	}
	if got := body.Requests[0]["cache_creation_input_tokens"]; got != float64(7) {
		t.Errorf("cache_creation_input_tokens = %#v, want 7", got)
	}
	if got := body.Requests[0]["cache_read_input_tokens"]; got != float64(11) {
		t.Errorf("cache_read_input_tokens = %#v, want 11", got)
	}
	breakdown, ok := body.Requests[0]["cost_breakdown"].(map[string]any)
	if !ok {
		t.Fatalf("cost_breakdown missing or wrong type: %s", w.Body.String())
	}
	if breakdown["input"] != 0.003 || breakdown["output"] != 0.012 || breakdown["total"] != 0.015 || breakdown["multiplier"] != 1.5 {
		t.Errorf("unexpected cost_breakdown: %#v", breakdown)
	}
}

// A user with no keys must yield a non-nil empty scope (never all traffic).
func TestRequestsHandlerEmptyKeysScopesToEmpty(t *testing.T) {
	keys := &fakeKeyStore{list: nil}
	store := &fakeRequestStore{}
	r := engineWithUser(func(r *gin.Engine) {
		r.GET("/requests", portal.RequestsHandler(store, keys))
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/requests", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if store.gotNames == nil || len(store.gotNames) != 0 {
		t.Errorf("no-keys user must scope to a non-nil EMPTY name set, got %#v", store.gotNames)
	}
}
