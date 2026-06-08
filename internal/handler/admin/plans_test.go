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
)

type fakePlanStore struct {
	plans       []repository.Plan
	createID    int64
	grantID     int64
	updateErr   error
	lastID      int64
	lastUserID  int64
	lastPlanID  int64
	lastPlan    repository.Plan
	lastStartAt time.Time
}

func (f *fakePlanStore) ListPlans(context.Context) ([]repository.Plan, error) {
	return f.plans, nil
}
func (f *fakePlanStore) CreatePlan(_ context.Context, p repository.Plan) (int64, error) {
	f.lastPlan = p
	return f.createID, nil
}
func (f *fakePlanStore) UpdatePlan(_ context.Context, id int64, p repository.Plan) error {
	f.lastID, f.lastPlan = id, p
	return f.updateErr
}
func (f *fakePlanStore) GrantPlanToUser(_ context.Context, userID, planID int64, startsAt time.Time) (int64, error) {
	f.lastUserID, f.lastPlanID, f.lastStartAt = userID, planID, startsAt
	return f.grantID, nil
}

func newPlanEngine(store *fakePlanStore) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/admin/api/plans", handler.ListPlansHandler(store))
	r.POST("/admin/api/plans", handler.CreatePlanHandler(store))
	r.PATCH("/admin/api/plans/:id", handler.UpdatePlanHandler(store))
	r.POST("/admin/api/users/:id/plan-grants", handler.GrantPlanToUserHandler(store))
	return r
}

func TestCreatePlanRejectsNegativePrice(t *testing.T) {
	rec := do(newPlanEngine(&fakePlanStore{}), http.MethodPost,
		"/admin/api/plans", `{"name":"Starter","price_usd":-1,"included_credit_usd":10,"price_ratio":1}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestListPlansReturnsRows(t *testing.T) {
	store := &fakePlanStore{plans: []repository.Plan{{ID: 1, Name: "Starter", PriceUSD: 9, IncludedCreditUSD: 10, PriceRatio: 1, Enabled: true}}}
	rec := do(newPlanEngine(store), http.MethodGet, "/admin/api/plans", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Plans []repository.Plan `json:"plans"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Plans) != 1 || resp.Plans[0].Name != "Starter" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestGrantPlanToUserReturnsGrant(t *testing.T) {
	store := &fakePlanStore{grantID: 7}
	rec := do(newPlanEngine(store), http.MethodPost,
		"/admin/api/users/42/plan-grants", `{"plan_id":3}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if store.lastUserID != 42 || store.lastPlanID != 3 || store.lastStartAt.IsZero() {
		t.Fatalf("grant not forwarded: user=%d plan=%d starts=%s", store.lastUserID, store.lastPlanID, store.lastStartAt)
	}
	var resp struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.ID != 7 {
		t.Fatalf("id = %d, want 7", resp.ID)
	}
}
